package web

import "embed"

// Assets объединяет встроенные шаблоны административного UI и статические ресурсы.
//
//go:embed templates/**/*.html static/**/*
var Assets embed.FS
