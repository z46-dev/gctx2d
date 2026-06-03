struct VertexIn {
    @location(0) position: vec2<f32>,
    @location(1) local: vec2<f32>,
    @location(2) half_size: vec2<f32>,
    @location(3) uv: vec2<f32>,
    @location(4) uv_min: vec2<f32>,
    @location(5) uv_max: vec2<f32>,
    @location(6) color: vec4<f32>,
    @location(7) shadow_color: vec4<f32>,
    @location(8) radius: f32,
    @location(9) kind: f32,
    @location(10) stroke_width: f32,
    @location(11) effect_time: f32,
    @location(12) shadow_blur: f32,
};

struct VertexOut {
    @builtin(position) clip_position: vec4<f32>,
    @location(0) local: vec2<f32>,
    @location(1) half_size: vec2<f32>,
    @location(2) uv: vec2<f32>,
    @location(3) uv_min: vec2<f32>,
    @location(4) uv_max: vec2<f32>,
    @location(5) color: vec4<f32>,
    @location(6) shadow_color: vec4<f32>,
    @location(7) radius: f32,
    @location(8) kind: f32,
    @location(9) stroke_width: f32,
    @location(10) effect_time: f32,
    @location(11) shadow_blur: f32,
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
    out.uv_min = input.uv_min;
    out.uv_max = input.uv_max;
    out.color = input.color;
    out.shadow_color = input.shadow_color;
    out.radius = input.radius;
    out.kind = input.kind;
    out.stroke_width = input.stroke_width;
    out.effect_time = input.effect_time;
    out.shadow_blur = input.shadow_blur;
    return out;
}

fn rounded_rect_sdf(p: vec2<f32>, half_size: vec2<f32>, radius: f32) -> f32 {
    let r = min(radius, min(half_size.x, half_size.y));
    let q = abs(p) - half_size + vec2<f32>(r, r);
    return length(max(q, vec2<f32>(0.0, 0.0))) + min(max(q.x, q.y), 0.0) - r;
}

fn compose_over(dst: vec4<f32>, src: vec4<f32>) -> vec4<f32> {
    let out_a = src.a + dst.a * (1.0 - src.a);
    if (out_a <= 0.0001) {
        return vec4<f32>(0.0, 0.0, 0.0, 0.0);
    }

    let out_rgb = (src.rgb * src.a + dst.rgb * dst.a * (1.0 - src.a)) / out_a;
    return vec4<f32>(out_rgb, out_a);
}

fn quad_uv(local: vec2<f32>, half_size: vec2<f32>, uv_min: vec2<f32>, uv_max: vec2<f32>) -> vec2<f32> {
    let safe_half = max(half_size, vec2<f32>(0.5, 0.5));
    let t = local / safe_half * 0.5 + vec2<f32>(0.5, 0.5);
    return mix(uv_min, uv_max, t);
}

fn inside_quad(local: vec2<f32>, half_size: vec2<f32>) -> f32 {
    let inside = abs(local.x) <= half_size.x && abs(local.y) <= half_size.y;
    return select(0.0, 1.0, inside);
}

fn sample_quad_alpha(input: VertexOut, uv: vec2<f32>) -> f32 {
    let clamped_uv = clamp(uv, input.uv_min, input.uv_max);
    let sample = textureSample(ui_atlas, ui_sampler, clamped_uv);
    return sample.a;
}

fn sample_shadow_alpha(input: VertexOut) -> f32 {
    if (input.shadow_blur <= 0.0 || input.shadow_color.a <= 0.0) {
        return 0.0;
    }

    let uv_span = input.uv_max - input.uv_min;
    let safe_size = max(input.half_size * 2.0, vec2<f32>(1.0, 1.0));
    let uv_per_pixel = uv_span / safe_size;
    let step_uv = uv_per_pixel * max(input.shadow_blur * 0.75, 1.0);
    let base_uv = quad_uv(input.local, input.half_size, input.uv_min, input.uv_max);

    var alpha = sample_quad_alpha(input, base_uv) * 0.227027;
    alpha += sample_quad_alpha(input, base_uv + vec2<f32>(step_uv.x, 0.0)) * 0.1945946;
    alpha += sample_quad_alpha(input, base_uv - vec2<f32>(step_uv.x, 0.0)) * 0.1945946;
    alpha += sample_quad_alpha(input, base_uv + vec2<f32>(0.0, step_uv.y)) * 0.1945946;
    alpha += sample_quad_alpha(input, base_uv - vec2<f32>(0.0, step_uv.y)) * 0.1945946;
    alpha += sample_quad_alpha(input, base_uv + step_uv) * 0.1216216;
    alpha += sample_quad_alpha(input, base_uv - step_uv) * 0.1216216;
    alpha += sample_quad_alpha(input, base_uv + vec2<f32>(step_uv.x, -step_uv.y)) * 0.1216216;
    alpha += sample_quad_alpha(input, base_uv + vec2<f32>(-step_uv.x, step_uv.y)) * 0.1216216;

    return clamp(alpha * 0.4, 0.0, 1.0);
}

@fragment
fn fs_main(input: VertexOut) -> @location(0) vec4<f32> {
    var color = input.color;

    // kind < 0.5: solid/SDF rectangle geometry.
    // 0.5 <= kind < 1.5: text glyph alpha mask.
    // kind >= 1.5: full-color image quad.
    if (input.kind < 0.5) {
        let d = rounded_rect_sdf(input.local, input.half_size, input.radius);
        let aa = max(length(vec2<f32>(dpdx(d), dpdy(d))), 0.5);

        var coverage: f32;
        var shadow_distance: f32;
        if (input.stroke_width > 0.0) {
            let half_stroke = max(input.stroke_width * 0.5, 0.5);
            coverage = 1.0 - smoothstep(half_stroke - aa, half_stroke + aa, abs(d));
            shadow_distance = abs(d) - half_stroke;
        } else {
            coverage = 1.0 - smoothstep(-aa, aa, d);
            shadow_distance = d;
        }

        var out_color = vec4<f32>(0.0, 0.0, 0.0, 0.0);
        if (input.shadow_blur > 0.0 && input.shadow_color.a > 0.0) {
            let shadow_coverage = 1.0 - smoothstep(0.0, input.shadow_blur + aa, max(shadow_distance, 0.0));
            out_color = vec4<f32>(input.shadow_color.rgb, input.shadow_color.a * shadow_coverage);
        }

        color.a = color.a * coverage;
        return compose_over(out_color, color);
    }

    let inside = inside_quad(input.local, input.half_size);
    let uv = quad_uv(input.local, input.half_size, input.uv_min, input.uv_max);
    let sample = textureSample(ui_atlas, ui_sampler, clamp(uv, input.uv_min, input.uv_max));
    var shadow = vec4<f32>(0.0, 0.0, 0.0, 0.0);
    if (input.shadow_blur > 0.0 && input.shadow_color.a > 0.0) {
        shadow = vec4<f32>(input.shadow_color.rgb, input.shadow_color.a * sample_shadow_alpha(input));
    }

    if (input.kind < 1.5) {
        color.a = color.a * sample.a * inside;
        return compose_over(shadow, color);
    }

    var image = sample * color * inside;
    return compose_over(shadow, image);
}
