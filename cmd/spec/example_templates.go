package main

import (
	"github.com/RHEnVision/provisioning-backend/internal/clients"
	"github.com/RHEnVision/provisioning-backend/internal/payloads"
)

var LaunchTemplateListResponse = []payloads.LaunchTemplateResponse{{
	Templates: []*clients.LaunchTemplate{
		{
			ID:   "lt-9843797432897342",
			Name: "XXL large backend API",
		},
	},
	NextToken: "",
}}
