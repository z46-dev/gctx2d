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
