// Brightening, ported from the Go windowed client (client/internal/render):
// a gamma curve that lifts dark colors a lot and bright ones barely, so dim
// terrain becomes legible without anything clipping to white, and colors
// keep their order.

/** Each channel v becomes 255 * (v/255)^(1/amount). amount 1 leaves colors
 * as they are; larger is brighter. */
export function brighten(hex: string, amount: number): string {
  if (amount <= 0 || amount === 1) return hex;
  const lift = (v: number) => Math.round(255 * Math.pow(v / 255, 1 / amount));
  const ch = (i: number) =>
    lift(parseInt(hex.slice(i, i + 2), 16))
      .toString(16)
      .padStart(2, "0");
  return `#${ch(1)}${ch(3)}${ch(5)}`;
}
