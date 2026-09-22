# NEXT-RELEASE

This release fixes a message-pool buffer leak in the provider's connection path, and carries a clearer test suite.

## What's Changed

### Fixed

- **Message-pool buffer leak on connect failure** ([#126](https://github.com/full-bars/meso-miner/pull/126)): when the provider tries to reach an upstream peer before the connection is established and that attempt fails, the pooled packet was released without returning its buffer to the pool. On a node where many dials fail, the pool grows without bound. The dial-failure branch now returns the buffer exactly once, with a regression test. Restart a provider to reclaim memory the leak already took.

### Maintenance

- **Deterministic test suite** ([#124](https://github.com/full-bars/meso-miner/pull/124)): the reload-trigger and health-registry tests no longer depend on timing or shared state.
- **Security blocklist sync** ([#125](https://github.com/full-bars/meso-miner/pull/125)): refreshed the content-filtering blocklist from upstream.
