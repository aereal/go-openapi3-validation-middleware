package openapi3middleware

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
)

type middleware = func(next http.Handler) http.Handler

// WithValidation returns a middleware that validates against both request and response.
func WithValidation(router routers.Router) middleware {
	req := WithRequestValidation(router)
	resp := WithResponseValidation(router)
	return func(next http.Handler) http.Handler {
		return req(resp(next))
	}
}

// WithResponseValidation returns a middleware that validates against response.
// It may consume larger memory because it holds entire response body to validate it later.
func WithResponseValidation(router routers.Router) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			irw := newBufferingResponseWriter(w)
			next.ServeHTTP(irw, r)
			ri, err := buildRequestValidationInputFromRequest(router, r)
			if err != nil {
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
				reportValidationError(w, err)
				return
			}
			irw.emit()
		})
	}
}

// WithRequestValidation returns a middleware that validates against request.
// It immediately returns an error response and does not call next handler if validation failed.
func WithRequestValidation(router routers.Router) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			input, err := buildRequestValidationInputFromRequest(router, r)
			if err != nil {
				respondErrorJSON(w, http.StatusInternalServerError, err)
				return
			}
			ctx := r.Context()
			if err := openapi3filter.ValidateRequest(ctx, input); err != nil {
				reportValidationError(w, err)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func buildRequestValidationInputFromRequest(router routers.Router, r *http.Request) (*openapi3filter.RequestValidationInput, error) {
	route, pathParams, err := router.FindRoute(r)
	if err != nil {
		return nil, err
	}
	input := &openapi3filter.RequestValidationInput{
		Request:    r,
		PathParams: pathParams,
		Route:      route,
	}
	return input, nil
}

func reportValidationError(w http.ResponseWriter, err error) {
	w.Header().Set("content-type", "application/json")
	if err, ok := err.(*openapi3filter.RequestError); ok {
		reportRequestError(w, err)
		return
	}
	if err, ok := err.(*openapi3filter.ResponseError); ok {
		reportResponseError(w, err)
		return
	}
	respondErrorJSON(w, http.StatusBadRequest, err)
}

type report struct {
	Reason      string           `json:"reason"`
	Field       string           `json:"field"`
	Value       interface{}      `json:"value"`
	Schema      *openapi3.Schema `json:"schema"`
	OriginError string           `json:"origin,omitempty"`
}

func reportRequestError(w http.ResponseWriter, requestErr *openapi3filter.RequestError) {
	if schemaErr, ok := requestErr.Err.(*openapi3.SchemaError); ok {
		_ = respondJSON(w, http.StatusBadRequest, rootError{
			Error: errorAggregate{
				Request: toReport(schemaErr),
			}})
		return
	}
	respondErrorJSON(w, http.StatusBadRequest, requestErr)
}

func reportResponseError(w http.ResponseWriter, responseErr *openapi3filter.ResponseError) {
	if schemaErr, ok := responseErr.Err.(*openapi3.SchemaError); ok {
		_ = respondJSON(w, http.StatusInternalServerError, rootError{
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
	_ = respondJSON(w, statusCode, payload{Error: &errorStruct{Message: err.Error(), Kind: fmt.Sprintf("%T", err)}})
}

func respondJSON(w http.ResponseWriter, statusCode int, payload interface{}) error {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(statusCode)
	return json.NewEncoder(w).Encode(payload)
}
