package web

import "embed"

// Assets bundles the built-in admin UI templates and static resources.
//
//go:embed templates/**/*.html static/**/*
var Assets embed.FS
