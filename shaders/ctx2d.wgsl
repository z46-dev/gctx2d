struct VertexIn {
    @location(0) position: vec2<f32>,
    @location(1) local: vec2<f32>,
    @location(2) half_size: vec2<f32>,
    @location(3) uv: vec2<f32>,
    @location(4) color: vec4<f32>,
    @location(5) radius: f32,
    @location(6) kind: f32,
    @location(7) stroke_width: f32,
    @location(8) effect_time: f32,
};

struct VertexOut {
    @builtin(position) clip_position: vec4<f32>,
    @location(0) local: vec2<f32>,
    @location(1) half_size: vec2<f32>,
    @location(2) uv: vec2<f32>,
    @location(3) color: vec4<f32>,
    @location(4) radius: f32,
    @location(5) kind: f32,
    @location(6) stroke_width: f32,
    @location(7) effect_time: f32,
};

@group(0) @binding(0)
var ui_sampler: sampler;

@group(0) @binding(1)
var ui_atlas: texture_2d<f32>;

// The Vertex shader is really lazy, it just passes through all the vertex attributes to the fragment shader.

@vertex
fn vs_main(input: VertexIn) -> VertexOut {
    var out: VertexOut;
    out.clip_position = vec4<f32>(input.position, 0.0, 1.0);
    out.local = input.local;
    out.half_size = input.half_size;
    out.uv = input.uv;
    out.color = input.color;
    out.radius = input.radius;
    out.kind = input.kind;
    out.stroke_width = input.stroke_width;
    out.effect_time = input.effect_time;
    return out;
}

fn rounded_rect_sdf(p: vec2<f32>, half_size: vec2<f32>, radius: f32) -> f32 {
    let r = min(radius, min(half_size.x, half_size.y));
    let q = abs(p) - half_size + vec2<f32>(r, r);
    return length(max(q, vec2<f32>(0.0, 0.0))) + min(max(q.x, q.y), 0.0) - r;
}

@fragment
fn fs_main(input: VertexOut) -> @location(0) vec4<f32> {
    var color = input.color;

    // kind < 0.5: solid/SDF rectangle geometry.
    // 0.5 <= kind < 1.5: text glyph alpha mask.
    // kind >= 1.5: full-color image quad.
    if (input.kind < 0.5) {
        let d = rounded_rect_sdf(input.local, input.half_size, input.radius);
        let aa = max(fwidth(d), 1.0);

        var coverage: f32;
        if (input.stroke_width > 0.0) {
            let half_stroke = max(input.stroke_width * 0.5, 0.5);
            coverage = 1.0 - smoothstep(half_stroke - aa, half_stroke + aa, abs(d));
        } else {
            coverage = 1.0 - smoothstep(-aa, aa, d);
        }

        color.a = color.a * coverage;
        return color;
    }

    let sample = textureSample(ui_atlas, ui_sampler, input.uv);

    if (input.kind < 1.5) {
        color.a = color.a * sample.a;
        return color;
    }

    var image = sample * color;
    return image;
}
