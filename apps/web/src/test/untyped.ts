/**
 * A deliberately untyped test value.
 *
 * Contract tests build a valid wire payload (or a mocked query result) and
 * then mutate it into the shapes the code under test must reject: missing
 * fields, wrong types, extra keys. No model type describes "any payload,
 * including the invalid ones", and `unknown` would turn every mutation into a
 * cast.
 *
 * Tests name this alias instead of writing a bare `any`, so `no-explicit-any`
 * stays on everywhere else and every use can be found by name. Production
 * code may not import it (see `no-restricted-imports` in eslint.config.js).
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any -- the single sanctioned `any`; see the comment above
export type Untyped = any;
