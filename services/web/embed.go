package web

import "embed"

// FS holds candidate and admin static pages.
//
//go:embed candidate/* admin/*
var FS embed.FS
