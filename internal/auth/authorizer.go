package auth

import (
	"context"
	"distributed-systems/internal/http"
	"errors"

	"connectrpc.com/connect"
	"github.com/casbin/casbin/v2"
)

type Authorizer struct {
	enforcer *casbin.Enforcer
}

func New(model, policy string) *Authorizer {
	enf, err := casbin.NewEnforcer(model, policy)
	if err != nil {
		panic(err)
	}
	return &Authorizer{
		enforcer: enf,
	}
}

func (a *Authorizer) Enforce(sub, obj, act string) (bool, error) {
	return a.enforcer.Enforce(sub, obj, act)
}

func (a *Authorizer) Interceptor() connect.UnaryInterceptorFunc {
	interceptor := func(next connect.UnaryFunc) connect.UnaryFunc {
		return connect.UnaryFunc(func(
			ctx context.Context,
			req connect.AnyRequest,
		) (connect.AnyResponse, error) {
			user := http.GetAuthInfo(ctx)
			if ok, err := a.Enforce(user, "*", req.Spec().Procedure); err != nil {
				return nil, err
			} else if !ok {
				return nil, connect.NewError(
					connect.CodePermissionDenied,
					errors.New("permission denied"),
				)
			}
			return next(ctx, req)
		})
	}
	return connect.UnaryInterceptorFunc(interceptor)
}
