//go:build cosmos

package oracle

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cosmos/cosmos-sdk/client"
	legacyruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/stretchr/testify/require"
)

func TestRegisterGRPCGatewayRoutesRegistersAllOracleQueryHTTPPaths(t *testing.T) {
	mux := legacyruntime.NewServeMux()
	AppModuleBasic{}.RegisterGRPCGatewayRoutes(client.Context{}, mux)

	for _, path := range []string{
		"/lumera/oracle/v1/params",
		"/lumera/oracle/v1/price_feed/LAC-USD",
		"/lumera/oracle/v1/price_feeds",
		"/lumera/oracle/v1/aggregated_price/LAC-USD",
	} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, path, nil)

			mux.ServeHTTP(recorder, request)

			require.NotEqual(t, http.StatusNotFound, recorder.Code)
		})
	}
}
