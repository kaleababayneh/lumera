//go:build cosmos

package credits

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cosmos/cosmos-sdk/client"
	legacyruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/stretchr/testify/require"
)

func TestRegisterGRPCGatewayRoutesRegistersAllCreditsQueryHTTPPaths(t *testing.T) {
	mux := legacyruntime.NewServeMux()
	AppModuleBasic{}.RegisterGRPCGatewayRoutes(client.Context{}, mux)

	for _, path := range []string{
		"/lumera.credits.v1.Query/Lock",
		"/lumera.credits.v1.Query/Locks",
		"/lumera.credits.v1.Query/Hold",
		"/lumera.credits.v1.Query/Holds",
		"/lumera.credits.v1.Query/Params",
	} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, path, nil)

			mux.ServeHTTP(recorder, request)

			require.NotEqual(t, http.StatusNotFound, recorder.Code)
		})
	}
}
