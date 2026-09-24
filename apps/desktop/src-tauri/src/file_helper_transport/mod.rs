//! Single-use local transport, NOT human authorization or executable trust.
//! Expected peers must come from independently trusted/held launch identities.
//! Nothing here launches/elevates a process, opens project files, or executes a
//! request. Only the closed installed-pair bootstrap consumes this transport;
//! paths, request JSON, arbitrary pipe names and product callers remain refused.
pub(super) mod bootstrap;
pub(super) mod native;
pub(super) mod wire;

#[cfg(test)]
mod tests;
