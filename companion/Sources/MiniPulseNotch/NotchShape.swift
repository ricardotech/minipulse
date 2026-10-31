//  NotchShape.swift — MiniPulseNotch
//
//  The notch silhouette: inverted (concave) top corners flare the
//  panel into the menu bar so it reads as part of the hardware,
//  and the bottom corners use Apple's real `.continuous` curve so
//  the open panel reads as a genuine Dynamic Island rather than a
//  plain rounded card. The body is inset by `topRadius` per side
//  to leave room for the flare.
//
//  Adapted from Glance (https://github.com/jonnyoo/glance),
//  Copyright (c) 2026 Jonathan Zhou, MIT License — same license as
//  this project. The continuous-corner extraction below follows
//  Glance's approach: build a reference UnevenRoundedRectangle
//  with only the bottom corners rounded and read its emitted path
//  elements back out (there is no public API for one corner).
//  If a future OS changes that emission, we fall back to plain
//  quad curves and the panel still renders sanely.

import SwiftUI
import MiniPulseCore

struct NotchShape: Shape {
    var topRadius: CGFloat
    var bottomRadius: CGFloat
    var hasNotch: Bool

    var animatableData: AnimatablePair<CGFloat, CGFloat> {
        get { AnimatablePair(topRadius, bottomRadius) }
        set { topRadius = newValue.first; bottomRadius = newValue.second }
    }

    func path(in rect: CGRect) -> Path {
        hasNotch ? notchPath(in: rect) : pillPath(in: rect)
    }

    private func notchPath(in rect: CGRect) -> Path {
        let top = max(0, min(topRadius, rect.width / 2))
        let bottom = max(0, min(bottomRadius, min(rect.width / 2 - top, rect.height)))
        var path = Path()

        // Top-left: flare outward to meet the screen edge.
        path.move(to: CGPoint(x: rect.minX, y: rect.minY))
        path.addQuadCurve(to: CGPoint(x: rect.minX + top, y: rect.minY + top),
                          control: CGPoint(x: rect.minX + top, y: rect.minY))

        let bodyRect = CGRect(x: rect.minX + top, y: rect.minY,
                              width: rect.width - 2 * top, height: rect.height)
        if let corners = ContinuousCorner.bottomCorners(bodyRect: bodyRect, radius: bottom) {
            path.addLine(to: corners.leftEdgeReach)
            for s in corners.left { path.addCurve(to: s.to, control1: s.control1, control2: s.control2) }
            path.addLine(to: corners.bottomEdgeRightReach)
            for s in corners.right { path.addCurve(to: s.to, control1: s.control1, control2: s.control2) }
            path.addLine(to: CGPoint(x: rect.maxX - top, y: rect.minY + top))
        } else {
            path.addLine(to: CGPoint(x: rect.minX + top, y: rect.maxY - bottom))
            path.addQuadCurve(to: CGPoint(x: rect.minX + top + bottom, y: rect.maxY),
                              control: CGPoint(x: rect.minX + top, y: rect.maxY))
            path.addLine(to: CGPoint(x: rect.maxX - top - bottom, y: rect.maxY))
            path.addQuadCurve(to: CGPoint(x: rect.maxX - top, y: rect.maxY - bottom),
                              control: CGPoint(x: rect.maxX - top, y: rect.maxY))
            path.addLine(to: CGPoint(x: rect.maxX - top, y: rect.minY + top))
        }

        path.addQuadCurve(to: CGPoint(x: rect.maxX, y: rect.minY),
                          control: CGPoint(x: rect.maxX - top, y: rect.minY))
        path.closeSubpath()
        return path
    }

    private func pillPath(in rect: CGRect) -> Path {
        let limit = min(rect.width, rect.height) / 2
        let top = max(0, min(topRadius, limit))
        let bottom = max(0, min(bottomRadius, limit))
        return UnevenRoundedRectangle(topLeadingRadius: top, bottomLeadingRadius: bottom,
                                      bottomTrailingRadius: bottom, topTrailingRadius: top,
                                      style: .continuous).path(in: rect)
    }
}

private enum ContinuousCorner {
    struct Segment { let control1: CGPoint; let control2: CGPoint; let to: CGPoint }
    struct BottomCorners {
        let leftEdgeReach: CGPoint
        let left: [Segment]
        let bottomEdgeRightReach: CGPoint
        let right: [Segment]
    }

    static func bottomCorners(bodyRect: CGRect, radius: CGFloat) -> BottomCorners? {
        let reference = UnevenRoundedRectangle(topLeadingRadius: 0, bottomLeadingRadius: radius,
                                               bottomTrailingRadius: radius, topTrailingRadius: 0,
                                               style: .continuous).path(in: bodyRect)
        var elements: [Path.Element] = []
        reference.forEach { elements.append($0) }
        guard elements.count >= 9,
              case .line(let p1) = elements[1],
              case .curve(let p2, let c2a, let c2b) = elements[2],
              case .curve(let p3, let c3a, let c3b) = elements[3],
              case .curve(let p4, let c4a, let c4b) = elements[4],
              case .line(let l5) = elements[5],
              case .curve(let p6, let c6a, let c6b) = elements[6],
              case .curve(let p7, let c7a, let c7b) = elements[7],
              case .curve(let p8, let c8a, let c8b) = elements[8]
        else { return nil }
        // Reference traces right→left; ours needs left→right.
        return BottomCorners(
            leftEdgeReach: p8,
            left: [Segment(control1: c8b, control2: c8a, to: p7),
                   Segment(control1: c7b, control2: c7a, to: p6),
                   Segment(control1: c6b, control2: c6a, to: l5)],
            bottomEdgeRightReach: p4,
            right: [Segment(control1: c4b, control2: c4a, to: p3),
                    Segment(control1: c3b, control2: c3a, to: p2),
                    Segment(control1: c2b, control2: c2a, to: p1)])
    }
}
