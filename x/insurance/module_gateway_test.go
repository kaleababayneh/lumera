
package insurance

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/types/module"
	gogogrpc "github.com/cosmos/gogoproto/grpc"
	legacyruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/LumeraProtocol/lumera/x/insurance/keeper"
	"github.com/LumeraProtocol/lumera/x/insurance/types"
)

func TestRegisterGRPCGatewayRoutesRegistersInsuranceQueryHTTPPaths(t *testing.T) {
	mux := legacyruntime.NewServeMux()
	AppModuleBasic{}.RegisterGRPCGatewayRoutes(client.Context{}, mux)

	for _, path := range []string{
		"/lumera/insurance/v1/pool_status",
		"/lumera/insurance/v1/claims/claim-alpha",
		"/lumera/insurance/v1/claims",
		"/lumera/insurance/v1/params",
	} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, path, nil)

			mux.ServeHTTP(recorder, request)

			require.NotEqual(t, http.StatusNotFound, recorder.Code)
		})
	}
}

func TestRegisterServicesRegistersInsuranceQueryServer(t *testing.T) {
	cfg := newCaptureConfigurator()

	NewAppModule(keeper.Keeper{}).RegisterServices(cfg)

	require.Contains(t, cfg.msgServer.GetServiceInfo(), types.Msg_serviceDesc.ServiceName)
	require.Contains(t, cfg.queryServer.GetServiceInfo(), types.Query_serviceDesc.ServiceName)
}

type captureConfigurator struct {
	msgServer   *grpc.Server
	queryServer *grpc.Server
}

func newCaptureConfigurator() *captureConfigurator {
	return &captureConfigurator{
		msgServer:   grpc.NewServer(),
		queryServer: grpc.NewServer(),
	}
}

func (c *captureConfigurator) RegisterService(sd *grpc.ServiceDesc, ss interface{}) {
	c.queryServer.RegisterService(sd, ss)
}

func (c *captureConfigurator) Error() error { return nil }

func (c *captureConfigurator) MsgServer() gogogrpc.Server { return c.msgServer }

func (c *captureConfigurator) QueryServer() gogogrpc.Server { return c.queryServer }

func (c *captureConfigurator) RegisterMigration(string, uint64, module.MigrationHandler) error {
	return nil
}
