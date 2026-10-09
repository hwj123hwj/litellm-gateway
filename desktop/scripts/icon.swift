// Regenerate on macOS: swift scripts/icon.swift resources/icon.png
import AppKit

let size = NSSize(width: 1024, height: 1024)
let bitmap = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: 1024, pixelsHigh: 1024,
  bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
  colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
NSGraphicsContext.saveGraphicsState()
NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: bitmap)
let rect = NSRect(origin: .zero, size: size).insetBy(dx: 40, dy: 40)
NSColor(calibratedRed: 0.08, green: 0.10, blue: 0.105, alpha: 1).setFill()
NSBezierPath(roundedRect: rect, xRadius: 206, yRadius: 206).fill()
let text = "G" as NSString
let attributes: [NSAttributedString.Key: Any] = [
  .font: NSFont.systemFont(ofSize: 660, weight: .semibold),
  .foregroundColor: NSColor(calibratedRed: 0.898, green: 0.639, blue: 0.298, alpha: 1)
]
let extent = text.size(withAttributes: attributes)
text.draw(at: NSPoint(x: (1024 - extent.width) / 2, y: (1024 - extent.height) / 2), withAttributes: attributes)
NSGraphicsContext.restoreGraphicsState()
let png = bitmap.representation(using: .png, properties: [:])!
try FileManager.default.createDirectory(atPath: "resources", withIntermediateDirectories: true)
try png.write(to: URL(fileURLWithPath: CommandLine.arguments[1]))
