package openapi3middleware

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
)

type user struct {
	Name string `json:"name"`
	ID   string `json:"id"`
	Age  int    `json:"age"`
}

func TestWithValidation(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromFile("./testdata/user-account-service.openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatal(err)
	}

	testCases := []struct {
		name             string
		handler          http.Handler
		request          func(origin string) *http.Request
		routeErrReporter func(w http.ResponseWriter, r *http.Request, err error)
		reqErrReporter   func(w http.ResponseWriter, r *http.Request, err error)
		resErrReporter   func(w http.ResponseWriter, r *http.Request, err error)
	}{
		{
			name: "GET /users/{id}: ok",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("content-type", "application/json")
				_ = json.NewEncoder(w).Encode(user{Name: "aereal", Age: 17, ID: "123"})
			}),
			request: func(origin string) *http.Request {
				return mustRequest(newRequest(http.MethodGet, origin+"/users/123", map[string]string{}, ""))
			},
		},
		{
			name: "GET /users/{id}: response error",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("content-type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": "aereal", "age": 17})
			}),
			request: func(origin string) *http.Request {
				return mustRequest(newRequest(http.MethodGet, origin+"/users/123", map[string]string{}, ""))
			},
		},
		{
			name: "GET /users/{id}: response error with custom error handler",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("content-type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": "aereal", "age": 17})
			}),
			request: func(origin string) *http.Request {
				return mustRequest(newRequest(http.MethodGet, origin+"/users/123", map[string]string{}, ""))
			},
			resErrReporter: func(w http.ResponseWriter, r *http.Request, err error) {
				requestNonNil := r != nil
				_, errTypeOK := err.(*openapi3filter.ResponseError)
				w.Header().Set("content-type", "text/plain")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = fmt.Fprintf(w, "the custom response validation error handler is called: errTypeOK=%t, request=%t", errTypeOK, requestNonNil)
			},
		},
		{
			name: "GET /unknown: find route error (not found)",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("content-type", "application/json")
				_ = json.NewEncoder(w).Encode(user{Name: "aereal", Age: 17, ID: "123"})
			}),
			request: func(origin string) *http.Request {
				return mustRequest(newRequest(http.MethodGet, origin+"/unknown", map[string]string{}, ""))
			},
			routeErrReporter: func(w http.ResponseWriter, r *http.Request, err error) {
				requestNonNil := r != nil
				errTypeOK := errors.Is(err, routers.ErrPathNotFound)
				w.Header().Set("content-type", "text/plain")
				w.WriteHeader(http.StatusNotFound)
				_, _ = fmt.Fprintf(w, "the custom find route error handler is called: errTypeOK=%t, request=%t", errTypeOK, requestNonNil)
			},
		},
		{
			name: "GET /users: find route error (method not allowed)",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("content-type", "application/json")
				_ = json.NewEncoder(w).Encode(user{Name: "aereal", Age: 17, ID: "123"})
			}),
			request: func(origin string) *http.Request {
				return mustRequest(newRequest(http.MethodGet, origin+"/users", map[string]string{}, ""))
			},
			routeErrReporter: func(w http.ResponseWriter, r *http.Request, err error) {
				requestNonNil := r != nil
				errTypeOK := errors.Is(err, routers.ErrMethodNotAllowed)
				w.Header().Set("content-type", "text/plain")
				w.WriteHeader(http.StatusMethodNotAllowed)
				_, _ = fmt.Fprintf(w, "the custom find route error handler is called: errTypeOK=%t, request=%t", errTypeOK, requestNonNil)
			},
		},
		{
			name: "POST /users: ok",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("content-type", "application/json")
				_ = json.NewEncoder(w).Encode(user{Name: "aereal", Age: 17, ID: "123"})
			}),
			request: func(origin string) *http.Request {
				return mustRequest(newRequest(http.MethodPost, origin+"/users", map[string]string{"content-type": "application/json"}, `{"name":"aereal","age":17}`))
			},
		},
		{
			name: "POST /users: request error",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				panic("should not reach here")
			}),
			request: func(origin string) *http.Request {
				return mustRequest(newRequest(http.MethodPost, origin+"/users", map[string]string{"content-type": "application/json"}, `{"name":"aereal","age":"abc"}`))
			},
		},
		{
			name: "POST /users: request error with custom error handler",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				panic("should not reach here")
			}),
			request: func(origin string) *http.Request {
				return mustRequest(newRequest(http.MethodPost, origin+"/users", map[string]string{"content-type": "application/json"}, `{"name":"aereal","age":"abc"}`))
			},
			reqErrReporter: func(w http.ResponseWriter, r *http.Request, err error) {
				requestNonNil := r != nil
				_, errTypeOK := err.(*openapi3filter.RequestError)
				w.Header().Set("content-type", "text/plain")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprintf(w, "the custom response validation error handler is called: errTypeOK=%t, request=%t", errTypeOK, requestNonNil)
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			mw := WithValidation(MiddlewareOptions{
				Router:                        router,
				ReportFindRouteError:          testCase.routeErrReporter,
				ReportRequestValidationError:  testCase.reqErrReporter,
				ReportResponseValidationError: testCase.resErrReporter,
			})
			srv := httptest.NewServer(mw(testCase.handler))
			defer srv.Close()
			gotResp, err := srv.Client().Do(testCase.request(srv.URL))
			if err != nil {
				t.Fatal(err)
			}
			expectedResp, err := resumeResponse(t.Name(), gotResp)
			if err != nil {
				t.Fatal(err)
			}
			if err := testResponse(expectedResp, gotResp); err != nil {
				t.Error(err)
			}
		})
	}
}

func resumeResponse(testName string, got *http.Response) (*http.Response, error) {
	imported, err := importResponse(testName)
	if err == nil {
		return imported, nil
	}
	// skip ErrNotExist
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := exportResponse(testName, got); err != nil {
		return nil, err
	}
	return got, nil
}

func responseDataPath(testName string) string {
	return filepath.Join("./testdata", url.QueryEscape(testName))
}

func importResponse(testName string) (*http.Response, error) {
	f, err := os.Open(responseDataPath(testName))
	if err != nil {
		return nil, err
	}
	return http.ReadResponse(bufio.NewReader(f), nil)
}

func exportResponse(testName string, resp *http.Response) error {
	f, err := os.OpenFile(responseDataPath(testName), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	dumped, err := httputil.DumpResponse(resp, true)
	if err != nil {
		return fmt.Errorf("DumpResponse: %w", err)
	}
	if _, err := f.Write(dumped); err != nil {
		return err
	}
	return nil
}

func mustRequest(r *http.Request, err error) *http.Request {
	if err != nil {
		panic(err)
	}
	return r
}

func newRequest(method, path string, headers map[string]string, body string) (*http.Request, error) {
	req, err := http.NewRequest(method, path, strings.NewReader((body)))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req, nil
}

func testResponse(expected, got *http.Response) error {
	if got.StatusCode != expected.StatusCode {
		return fmt.Errorf("StatusCode: got=%d expected=%d", got.StatusCode, expected.StatusCode)
	}
	expectedBody, _ := io.ReadAll(expected.Body)
	gotBody, _ := io.ReadAll(got.Body)
	defer func() {
		// rewind body
		expected.Body = io.NopCloser(bytes.NewReader(expectedBody))
		got.Body = io.NopCloser(bytes.NewReader(gotBody))
	}()
	if string(expectedBody) != string(gotBody) {
		return fmt.Errorf("body:\ngot=%s\nexpected=%s", gotBody, expectedBody)
	}
	if err := testHTTPHeader(expected.Header, got.Header); err != nil {
		return err
	}
	return nil
}

func testHTTPHeader(expected, got http.Header) error {
	excludes := map[string]bool{"Date": true}
	expectedBuf := new(bytes.Buffer)
	gotBuf := new(bytes.Buffer)
	if err := expected.WriteSubset(expectedBuf, excludes); err != nil {
		return err
	}
	if err := got.WriteSubset(gotBuf, excludes); err != nil {
		return err
	}
	if expectedBuf.String() != gotBuf.String() {
		return fmt.Errorf("got=%q expected=%q", gotBuf.String(), expectedBuf.String())
	}
	return nil
}
