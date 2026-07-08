// Playable-area bounds, shared by the rover clamp (controls.ts) and the
// challenge site placement (world/gen/sites.ts) so nothing ever spawns
// beyond the invisible wall the player can actually reach.

/** Half-size of the playable square: the rover is clamped to ±PLAYABLE_BOUND. */
export const PLAYABLE_BOUND = 240;

/** Challenge sites stay comfortably inside the wall (pad + marker visible). */
export const SITE_BOUND = PLAYABLE_BOUND * 0.9;
