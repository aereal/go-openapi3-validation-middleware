package openapi3middleware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

type middleware = func(next http.Handler) http.Handler

type MiddlewareOptions struct {
	Router                        routers.Router
	ValidationOptions             *openapi3filter.Options
	ReportFindRouteError          func(w http.ResponseWriter, r *http.Request, err error)
	ReportRequestValidationError  func(w http.ResponseWriter, r *http.Request, err error)
	ReportResponseValidationError func(w http.ResponseWriter, r *http.Request, err error)
	TracerProvider                trace.TracerProvider
}

func (o MiddlewareOptions) reportFindRouteError(w http.ResponseWriter, r *http.Request, err error) {
	if f := o.ReportFindRouteError; f != nil {
		f(w, r, err)
		return
	}
	defaultReportFindRouteError(w, err)
}

func (o MiddlewareOptions) reportReqError(w http.ResponseWriter, r *http.Request, err error) {
	if f := o.ReportRequestValidationError; f != nil {
		f(w, r, err)
		return
	}
	defaultReportRequestError(w, err)
}

func (o MiddlewareOptions) reportRespError(w http.ResponseWriter, r *http.Request, err error) {
	if f := o.ReportResponseValidationError; f != nil {
		f(w, r, err)
		return
	}
	defaultReportResponseError(w, err)
}

// WithValidation returns a middleware that validates against both request and response.
func WithValidation(options MiddlewareOptions) middleware {
	req := WithRequestValidation(options)
	resp := WithResponseValidation(options)
	return func(next http.Handler) http.Handler {
		return req(resp(next))
	}
}

// WithResponseValidation returns a middleware that validates against response.
// It may consume larger memory because it holds entire response body to validate it later.
func WithResponseValidation(options MiddlewareOptions) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			ctx, span := getTracer(ctx, options).Start(ctx, "ResponseValidation")
			defer span.End()
			irw := newBufferingResponseWriter(w)
			next.ServeHTTP(irw, r.WithContext(ctx))
			ri, err := buildRequestValidationInputFromRequest(options.Router, r, options.ValidationOptions)
			if frErr := new(findRouteErr); errors.As(err, &frErr) {
				actualErr := frErr.Unwrap()
				span.RecordError(actualErr)
				options.reportFindRouteError(w, r, actualErr)
				return
			} else if err != nil {
				span.RecordError(err)
				respondErrorJSON(w, http.StatusInternalServerError, err)
				return
			}
			input := &openapi3filter.ResponseValidationInput{
				RequestValidationInput: ri,
				Status:                 irw.statusCode,
				Header:                 irw.Header(),
			}
			if input.Status == 0 {
				input.Status = http.StatusOK
			}
			bodyBytes := irw.buf.Bytes()
			input.SetBodyBytes(bodyBytes)
			if err := openapi3filter.ValidateResponse(ctx, input); err != nil {
				span.RecordError(err)
				options.reportRespError(w, r, err)
				return
			}
			irw.emit()
		})
	}
}

// WithRequestValidation returns a middleware that validates against request.
// It immediately returns an error response and does not call next handler if validation failed.
func WithRequestValidation(options MiddlewareOptions) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			ctx, span := getTracer(ctx, options).Start(ctx, "RequestValidation")
			defer span.End()
			input, err := buildRequestValidationInputFromRequest(options.Router, r, options.ValidationOptions)
			if frErr := new(findRouteErr); errors.As(err, &frErr) {
				actualErr := frErr.Unwrap()
				span.RecordError(actualErr)
				options.reportFindRouteError(w, r, actualErr)
				return
			} else if err != nil {
				span.RecordError(err)
				respondErrorJSON(w, http.StatusInternalServerError, err)
				return
			}
			if err := openapi3filter.ValidateRequest(ctx, input); err != nil {
				span.RecordError(err)
				options.reportReqError(w, r, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

type findRouteErr struct {
	err error
}

func (e *findRouteErr) Unwrap() error {
	return e.err
}

func (e *findRouteErr) Error() string {
	return e.err.Error()
}

func buildRequestValidationInputFromRequest(router routers.Router, r *http.Request, options *openapi3filter.Options) (*openapi3filter.RequestValidationInput, error) {
	route, pathParams, err := router.FindRoute(r)
	if err != nil {
		return nil, &findRouteErr{err: err}
	}
	input := &openapi3filter.RequestValidationInput{
		Request:    r,
		PathParams: pathParams,
		Route:      route,
		Options:    options,
	}
	return input, nil
}

type report struct {
	Reason      string           `json:"reason"`
	Field       string           `json:"field"`
	Value       interface{}      `json:"value"`
	Schema      *openapi3.Schema `json:"schema"`
	OriginError string           `json:"origin,omitempty"`
}

func defaultReportFindRouteError(w http.ResponseWriter, err error) {
	respondErrorJSON(w, http.StatusInternalServerError, err)
}

func defaultReportRequestError(w http.ResponseWriter, err error) {
	requestErr := new(openapi3filter.RequestError)
	if !errors.As(err, &requestErr) {
		return
	}
	schemaErr := new(openapi3.SchemaError)
	if errors.As(requestErr.Err, &schemaErr) {
		_ = respondJSON(w, http.StatusBadRequest, rootError{ //nolint:errcheck
			Error: errorAggregate{
				Request: toReport(schemaErr),
			}})
		return
	}
	respondErrorJSON(w, http.StatusBadRequest, requestErr)
}

func defaultReportResponseError(w http.ResponseWriter, err error) {
	responseErr := new(openapi3filter.ResponseError)
	if !errors.As(err, &responseErr) {
		return
	}
	if schemaErr := new(openapi3.SchemaError); errors.As(responseErr.Err, &schemaErr) {
		_ = respondJSON(w, http.StatusInternalServerError, rootError{ //nolint:errcheck
			Error: errorAggregate{
				Response: toReport(schemaErr),
			}})
		return
	}
	respondErrorJSON(w, http.StatusInternalServerError, responseErr)
}

type rootError struct {
	Error errorAggregate `json:"error"`
}

type errorAggregate struct {
	Request  *report `json:"request,omitempty"`
	Response *report `json:"response,omitempty"`
}

func toReport(schemaErr *openapi3.SchemaError) *report {
	if schemaErr == nil {
		return nil
	}
	return &report{
		Reason: schemaErr.Reason,
		Field:  schemaErr.SchemaField,
		Value:  schemaErr.Value,
		Schema: schemaErr.Schema,
	}
}

func respondErrorJSON(w http.ResponseWriter, statusCode int, err error) {
	type errorStruct struct {
		Message string
		Kind    string
	}
	type payload struct {
		Error *errorStruct
	}
	_ = respondJSON(w, statusCode, payload{Error: &errorStruct{Message: err.Error(), Kind: fmt.Sprintf("%T", err)}}) //nolint:errcheck
}

func respondJSON(w http.ResponseWriter, statusCode int, payload interface{}) error {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(statusCode)
	return json.NewEncoder(w).Encode(payload)
}

const tracerName = "github.com/aereal/go-openapi3-validation-middleware"

func getTracer(ctx context.Context, opts MiddlewareOptions) trace.Tracer {
	tp := opts.TracerProvider
	if tp == nil {
		if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
			tp = span.TracerProvider()
		} else {
			tp = otel.GetTracerProvider()
		}
	}
	return tp.Tracer(tracerName)
}
