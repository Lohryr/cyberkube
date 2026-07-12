import { describe, it, expect } from "vitest";
import { Rover } from "./controls";

// `Rover` reads input from a private `keys` Set populated by real
// KeyboardEvent listeners. For deterministic, DOM-free testing we reach into
// the same physical key codes it already listens for via `pressKey`/
// `releaseKey`, added as a minimal test seam (no behavioral change to
// `update()`).

describe("Rover.update() steering while reversing", () => {
  it("turning left while driving forward curves the heading one way", () => {
    const rover = new Rover();
    rover.pressKey("KeyW"); // forward
    for (let i = 0; i < 30; i++) rover.update(1 / 60); // build up forward velocity
    rover.releaseKey("KeyW");

    const headingBefore = rover.facing;
    rover.pressKey("KeyA"); // turn left
    rover.update(1 / 60);
    const headingAfter = rover.facing;

    expect(rover.velocity).toBeGreaterThan(0);
    expect(headingAfter).not.toBe(headingBefore);
    expect(Math.sign(headingAfter - headingBefore)).toBe(1);
  });

  it("turning right while driving forward curves the heading the other way", () => {
    const rover = new Rover();
    rover.pressKey("KeyW");
    for (let i = 0; i < 30; i++) rover.update(1 / 60);
    rover.releaseKey("KeyW");

    const headingBefore = rover.facing;
    rover.pressKey("KeyD"); // turn right
    rover.update(1 / 60);
    const headingAfter = rover.facing;

    expect(rover.velocity).toBeGreaterThan(0);
    expect(Math.sign(headingAfter - headingBefore)).toBe(-1);
  });

  it("turning left while reversing swings the nose to the screen-left, matching forward feel", () => {
    const forwardRover = new Rover();
    forwardRover.pressKey("KeyW");
    for (let i = 0; i < 30; i++) forwardRover.update(1 / 60);
    forwardRover.releaseKey("KeyW");
    const forwardHeadingBefore = forwardRover.facing;
    forwardRover.pressKey("KeyA");
    forwardRover.update(1 / 60);
    const forwardTurnSign = Math.sign(forwardRover.facing - forwardHeadingBefore);

    const reverseRover = new Rover();
    reverseRover.pressKey("KeyS"); // reverse
    for (let i = 0; i < 30; i++) reverseRover.update(1 / 60);
    reverseRover.releaseKey("KeyS");
    expect(reverseRover.velocity).toBeLessThan(0);

    const reverseHeadingBefore = reverseRover.facing;
    reverseRover.pressKey("KeyA"); // turn left, while reversing
    reverseRover.update(1 / 60);
    const reverseTurnSign = Math.sign(reverseRover.facing - reverseHeadingBefore);

    // The felt/screen turn direction (derived from the swing of the
    // position delta, not the raw heading sign) must match the forward
    // case: pressing "turn left" always curves the path to the same
    // perceived side, whether driving forward or backward.
    const forwardLateralDelta = Math.sin(forwardRover.facing) * forwardRover.velocity;
    const reverseLateralDelta = Math.sin(reverseRover.facing) * reverseRover.velocity;
    expect(Math.sign(forwardLateralDelta)).toBe(Math.sign(reverseLateralDelta));

    // Raw heading sign is expected to be inverted while reversing — that's
    // exactly the fix: the effective steering contribution flips sign so
    // the *felt* direction (checked above) stays consistent.
    expect(reverseTurnSign).toBe(-forwardTurnSign);
  });

  it("turning right while reversing swings the nose to the screen-right, matching forward feel", () => {
    const forwardRover = new Rover();
    forwardRover.pressKey("KeyW");
    for (let i = 0; i < 30; i++) forwardRover.update(1 / 60);
    forwardRover.releaseKey("KeyW");
    const forwardHeadingBefore = forwardRover.facing;
    forwardRover.pressKey("KeyD");
    forwardRover.update(1 / 60);
    const forwardTurnSign = Math.sign(forwardRover.facing - forwardHeadingBefore);

    const reverseRover = new Rover();
    reverseRover.pressKey("KeyS");
    for (let i = 0; i < 30; i++) reverseRover.update(1 / 60);
    reverseRover.releaseKey("KeyS");
    expect(reverseRover.velocity).toBeLessThan(0);

    const reverseHeadingBefore = reverseRover.facing;
    reverseRover.pressKey("KeyD");
    reverseRover.update(1 / 60);
    const reverseTurnSign = Math.sign(reverseRover.facing - reverseHeadingBefore);

    const forwardLateralDelta = Math.sin(forwardRover.facing) * forwardRover.velocity;
    const reverseLateralDelta = Math.sin(reverseRover.facing) * reverseRover.velocity;
    expect(Math.sign(forwardLateralDelta)).toBe(Math.sign(reverseLateralDelta));
    expect(reverseTurnSign).toBe(-forwardTurnSign);
  });
});
