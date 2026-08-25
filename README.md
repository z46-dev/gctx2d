![Tests](https://img.shields.io/github/actions/workflow/status/z46-dev/gctx2d/ci.yml?branch=main&event=push&label=CI)
![Made with Golang](https://img.shields.io/badge/-Made_with_Golang-007d9c?logo=go&logoColor=white)

# gctx2d

Canvas-style bindings for https://github.com/gogpu/gogpu wgpu pipeline

```go
canvas, err := gctx2d.NewOffscreenCanvas(device, 512, 512)
if err != nil {
	return err
}

defer canvas.Release()

ctx, err := canvas.GetContext()
if err != nil {
	return err
}

ctx.SetFillStyle(gctx2d.ColorWhite)
ctx.Rect(0, 0, 512, 512)
ctx.Fill()

ctx.SetShadowColor(gctx2d.Color{A: 0.35})
ctx.SetShadowBlur(12)
ctx.SetGlobalAlpha(0.9)
ctx.SetFillStyle(gctx2d.ColorBlack)
ctx.Circle(256, 256, 64)
ctx.Fill()

// Clip subsequent drawing to a path. Clip state participates in Save/Restore.
ctx.Save()
ctx.BeginPath()
ctx.RoundedRect(32, 32, 448, 448, 24)
ctx.Clip() // defaults to ClipModeIntersect
// ctx.Clip(gctx2d.ClipModeDifference) punches the current path out instead.
ctx.FillCircle(256, 256, 200)
ctx.Restore()

if err := canvas.Flush(nil); err != nil {
	return err
}

img := canvas.Image("hud-layer")
ctx2, err := anotherCanvas.GetContext()
if err != nil {
	return err
}

ctx2.DrawImageBounds(img, 32, 32, 256, 256)
```
