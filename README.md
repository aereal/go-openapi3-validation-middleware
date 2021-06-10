![CI][ci-status]
[![PkgGoDev][pkg-go-dev-badge]][pkg-go-dev]

# go-openapi3-validation-middleware

net/http middleware to validate HTTP requests/responses against OpenAPI 3 schema using [kin-openapi][].

## Installation

```sh
go get github.com/aereal/go-openapi3-validation-middleware
```

## Synopsis

```go
import (
	"net/http"

	"github.com/aereal/go-openapi3-validation-middleware"
	"github.com/getkin/kin-openapi/routers"
)

func main() {
	var router routers.Router // must be built with certain way
	mw := openapi3middleware.WithValidation(router)
	http.Handle("/", mw(http.HandlerFunc(func (w http.ResponseWriter, r *http.Request) {
		// this handler is called if validation succeeds
	})))
}
```

## License

See LICENSE file.

[pkg-go-dev]: https://pkg.go.dev/github.com/aereal/go-openapi3-validation-middleware
[pkg-go-dev-badge]: https://pkg.go.dev/badge/aereal/go-openapi3-validation-middleware
[ci-status]: https://github.com/aereal/go-openapi3-validation-middleware/workflows/CI/badge.svg?branch=main
[kin-openapi]: https://github.com/getkin/kin-openapi
