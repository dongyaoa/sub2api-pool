package admin

import "github.com/Wei-Shaw/sub2api/internal/service"

// SetOpenAIReauthService attaches the verified manual recovery exit without
// changing the public account handler constructor used by other integrations.
func (h *AccountHandler) SetOpenAIReauthService(reauth *service.OpenAIReauthService) {
	h.openAIReauth = reauth
}
