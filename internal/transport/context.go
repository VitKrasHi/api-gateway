package transport

import "context"

type ctxKey string

const (
	ctxRequestID ctxKey = "request_id"
	ctxUserID    ctxKey = "user_id"
	ctxUserRoles ctxKey = "user_roles"
)

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxRequestID, id)
}

func RequestID(ctx context.Context) string {
	if v, ok := ctx.Value(ctxRequestID).(string); ok {
		return v
	}
	return ""
}

func WithUser(ctx context.Context, id string, roles []string) context.Context {
	ctx = context.WithValue(ctx, ctxUserID, id)
	ctx = context.WithValue(ctx, ctxUserRoles, roles)
	return ctx
}

func UserID(ctx context.Context) string {
	if v, ok := ctx.Value(ctxUserID).(string); ok {
		return v
	}
	return ""
}

func UserRoles(ctx context.Context) []string {
	if v, ok := ctx.Value(ctxUserRoles).([]string); ok {
		return v
	}
	return nil
}
