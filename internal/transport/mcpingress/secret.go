package mcpingress

import "net/http"

type BearerInjector struct {
	apply      func(*http.Request)
	configured bool
}

func NewBearerInjector(token string) BearerInjector {
	if token == "" {
		return BearerInjector{apply: func(*http.Request) {}}
	}
	return BearerInjector{
		configured: true,
		apply: func(request *http.Request) {
			request.Header.Set("Authorization", "Bearer "+token)
		},
	}
}

func (injector BearerInjector) Apply(request *http.Request) {
	if injector.apply != nil {
		injector.apply(request)
	}
}

func (injector BearerInjector) Configured() bool { return injector.configured }
