package server

import "github.com/go-zoox/zoox"

func createAuthMiddleware(cfg *Config) func(ctx *zoox.Context) {
	return func(ctx *zoox.Context) {
		user, pass, ok := ctx.Request.BasicAuth()
		if !ok {
			ctx.Set("WWW-Authenticate", `Basic realm="go-zoox"`)
			ctx.Status(401)
			return
		}

		if !(user == cfg.Username && pass == cfg.Password) {
			ctx.Status(401)
			return
		}

		ctx.Next()
	}
}
