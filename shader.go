package gctx2d

import _ "embed"

//go:embed shaders/ctx2d.wgsl
var ctxWGSL string

//go:embed shaders/clip.wgsl
var clipWGSL string
