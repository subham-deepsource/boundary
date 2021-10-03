package controller

import (
	"context"
	"fmt"

	authpb "github.com/hashicorp/boundary/internal/gen/controller/auth"
	"github.com/hashicorp/boundary/internal/requests"
	"github.com/hashicorp/boundary/internal/servers/controller/auth"
	"github.com/mr-tron/base58"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

const (
	requestInfoMdKey = "request-info"
)

// requestCtxInterceptor creates an unary server interceptor that pulls grpc
// metadata into a ctx for the request.  The metadata must be set in an upstream
// http handler or middleware.  The current required metadata fields are:
// path, method, disableAuthzFailures, publicId, encryptedToken, tokenFormat
func requestCtxInterceptor(c *Controller) (grpc.UnaryServerInterceptor, error) {
	// Authorization unary interceptor function to handle authorize per RPC call
	return func(ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (interface{}, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, fmt.Errorf("missing metadata")
		}

		values := md.Get(requestInfoMdKey)
		if len(values) < 0 {
			return nil, fmt.Errorf("missing required metadata %s", requestInfoMdKey)
		}
		if len(values) > 1 {
			return nil, fmt.Errorf("expected 1 value for %s metadata and got %d", requestInfoMdKey, len(values))
		}

		decoded, err := base58.FastBase58Decoding(values[0])
		if err != nil {
			return nil, fmt.Errorf("unable to decode request info: %w", err)
		}
		var rpb authpb.RequestInfo
		if err := proto.Unmarshal(decoded, &rpb); err != nil {
			return nil, fmt.Errorf("unable to unmarshal request info: %w", err)
		}
		requestInfo := auth.ToStruct(&rpb)
		ctx = auth.NewVerifierContext(ctx, c.IamRepoFn, c.AuthTokenRepoFn, c.ServersRepoFn, c.kms, requestInfo)

		// Add general request information to the context. The information from
		// the auth verifier context is pretty specifically curated to
		// authentication/authorization verification so this is more
		// general-purpose.
		//
		// We could use requests.NewRequestContext but this saves an immediate
		// lookup.
		ctx = context.WithValue(ctx, requests.ContextRequestInformationKey, &requests.RequestContext{
			Path:   requestInfo.Path,
			Method: requestInfo.Method,
		})

		// Calls the handler
		h, err := handler(ctx, req)

		return h, err
	}, nil
}
