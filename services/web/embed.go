package web

import "embed"

// FS holds candidate, interviewer, and admin static pages.
//
//go:embed candidate/* interviewer/* admin/*
var FS embed.FS
