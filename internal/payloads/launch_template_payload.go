package payloads

import (
	"net/http"

	"github.com/RHEnVision/provisioning-backend/internal/clients"
	"github.com/go-chi/render"
)

// See clients.LaunchTemplate
type LaunchTemplateResponse struct {
	Templates []*clients.LaunchTemplate
	NextToken string
}

func (s *LaunchTemplateResponse) Bind(_ *http.Request) error {
	return nil
}

func (s *LaunchTemplateResponse) Render(_ http.ResponseWriter, _ *http.Request) error {
	return nil
}

func NewListLaunchTemplateResponse(sl []*clients.LaunchTemplate, nextToken string) render.Renderer {
	return &LaunchTemplateResponse{Templates: sl, NextToken: nextToken}
}
