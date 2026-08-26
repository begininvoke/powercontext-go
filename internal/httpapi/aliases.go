package httpapi

import "github.com/ogen-go/ogen/middleware"

// These aliases keep the request-ID middleware signature readable without
// introducing a transport abstraction parallel to ogen.
type Request = middleware.Request
type Response = middleware.Response
type Next = middleware.Next
type Middleware = middleware.Middleware
