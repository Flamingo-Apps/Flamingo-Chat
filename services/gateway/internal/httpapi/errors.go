package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// errorResponse is the wire shape docs/ARCHITECTURE.md §5.1 specifies:
// {error: {code, message}}.
type errorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// writeError maps a gRPC error (from any of the backend services) to an
// HTTP status and the documented JSON error shape. A plain Go error that
// isn't a gRPC status (status.FromError always succeeds, treating it as
// codes.Unknown) still gets a sensible response rather than leaking a Go
// error string as if it were an API contract.
func writeError(w http.ResponseWriter, err error) {
	st, _ := status.FromError(err)
	writeJSON(w, httpStatusFromCode(st.Code()), newErrorResponse(st.Code(), st.Message()))
}

func newErrorResponse(code codes.Code, message string) errorResponse {
	var resp errorResponse
	resp.Error.Code = codeString(code)
	resp.Error.Message = message
	return resp
}

// codeString renders a gRPC code the way an HTTP API caller expects:
// lowercase, underscore-separated ("not_found", not "NotFound").
func codeString(code codes.Code) string {
	s := code.String()
	var out strings.Builder
	out.Grow(len(s) + 4)
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			out.WriteRune('_')
		}
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		out.WriteRune(r)
	}
	return out.String()
}

// httpStatusFromCode is the standard gRPC-to-HTTP mapping grpc-gateway
// uses (github.com/grpc-ecosystem/grpc-gateway/runtime.HTTPStatusFromCode),
// verified against that source rather than assumed.
func httpStatusFromCode(code codes.Code) int {
	switch code {
	case codes.OK:
		return http.StatusOK
	case codes.Canceled:
		return 499
	case codes.InvalidArgument, codes.FailedPrecondition, codes.OutOfRange:
		return http.StatusBadRequest
	case codes.DeadlineExceeded:
		return http.StatusGatewayTimeout
	case codes.NotFound:
		return http.StatusNotFound
	case codes.AlreadyExists, codes.Aborted:
		return http.StatusConflict
	case codes.PermissionDenied:
		return http.StatusForbidden
	case codes.Unauthenticated:
		return http.StatusUnauthorized
	case codes.ResourceExhausted:
		return http.StatusTooManyRequests
	case codes.Unimplemented:
		return http.StatusNotImplemented
	case codes.Unavailable:
		return http.StatusServiceUnavailable
	case codes.Unknown, codes.Internal, codes.DataLoss:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
