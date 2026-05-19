package webhook

import "github.com/illegalstudio/lazyagent/internal/core"

// ConfigAdapter wraps a core.Config so it satisfies ConfigSource.
// It returns the valid + enabled webhooks at call time, allowing future
// config reloads to take effect on the next event.
type ConfigAdapter struct {
	Cfg core.Config
}

// Webhooks returns the validated + enabled webhooks from the wrapped config.
func (a *ConfigAdapter) Webhooks() []core.WebhookConfig {
	return a.Cfg.ValidWebhooks()
}
