package gctx2d

import _ "embed"

//go:embed shaders/ctx2d.wgsl
var ctxWGSL string

const clipWGSL = `
struct VertexIn {
    @location(0) position: vec2<f32>,
};

@vertex
fn vs_main(input: VertexIn) -> @builtin(position) vec4<f32> {
    return vec4<f32>(input.position, 0.0, 1.0);
}
`
