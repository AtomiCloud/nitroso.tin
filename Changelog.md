## [1.55.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.54.1...v1.55.0) (2026-07-17)


### ✨ Features ✨

* **prober:** add epoch probe-by-reserving fleet ([#41](https://github.com/AtomiCloud/nitroso.tin/issues/41)) ([1c0e8d5](https://github.com/AtomiCloud/nitroso.tin/commit/1c0e8d5a7b825efdc2bab1c90e91d1402b865017)), closes [#48](https://github.com/AtomiCloud/nitroso.tin/issues/48)

## [1.54.1](https://github.com/AtomiCloud/nitroso.tin/compare/v1.54.0...v1.54.1) (2026-07-15)


### 🐛 Bug Fixes 🐛

* refund the requested ticket, scope refund-amount fallbacks ([#47](https://github.com/AtomiCloud/nitroso.tin/issues/47)) ([2ce94d6](https://github.com/AtomiCloud/nitroso.tin/commit/2ce94d65d9d238e36a3a26402e52da15f7754fe9))

## [1.54.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.53.1...v1.54.0) (2026-07-15)


### ✨ Features ✨

* capture and backfill KTMB termination refunds ([#46](https://github.com/AtomiCloud/nitroso.tin/issues/46)) ([ae43a9c](https://github.com/AtomiCloud/nitroso.tin/commit/ae43a9ca01059a62737ee8d8a57812c0ae858508))

## [1.53.1](https://github.com/AtomiCloud/nitroso.tin/compare/v1.53.0...v1.53.1) (2026-07-14)


### 🐛 Bug Fixes 🐛

* refresh rejected KTMB backfill sessions ([#45](https://github.com/AtomiCloud/nitroso.tin/issues/45)) ([0aab212](https://github.com/AtomiCloud/nitroso.tin/commit/0aab21263e25d432abb5cb84974c8afe2fafb00a))

## [1.53.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.52.0...v1.53.0) (2026-07-14)


### ✨ Features ✨

* **buyer:** report and backfill actual KTMB costs ([#44](https://github.com/AtomiCloud/nitroso.tin/issues/44)) ([0b0e470](https://github.com/AtomiCloud/nitroso.tin/commit/0b0e470565dd9ed5b7a6438ddae3112d77f8d411))

## [1.52.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.51.0...v1.52.0) (2026-07-12)


### ✨ Features ✨

* **withdrawer:** runtime sweep gate from zinc settings ([#43](https://github.com/AtomiCloud/nitroso.tin/issues/43)) ([302cac3](https://github.com/AtomiCloud/nitroso.tin/commit/302cac3a8e3a82098a313fd54c7e7cc23104bc08))

## [1.51.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.50.1...v1.51.0) (2026-07-11)


### ✨ Features ✨

* **withdrawer:** config gate to disable the auto-approve sweep ([#42](https://github.com/AtomiCloud/nitroso.tin/issues/42)) ([9875fea](https://github.com/AtomiCloud/nitroso.tin/commit/9875fea0f015df3135cb1e984f1258cdb988c211))

## [1.50.1](https://github.com/AtomiCloud/nitroso.tin/compare/v1.50.0...v1.50.1) (2026-07-11)


### 🐛 Bug Fixes 🐛

* **recoverer:** gate KTMB not-found on semantic status, harden repair ([#40](https://github.com/AtomiCloud/nitroso.tin/issues/40)) ([7dcd0de](https://github.com/AtomiCloud/nitroso.tin/commit/7dcd0de2c087bfb2943d8731bf6a97c81a3718c1))

## [1.50.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.49.0...v1.50.0) (2026-07-10)


### ✨ Features ✨

* **recoverer:** retry-before-duplicate via zinc recycle + missing-ticket repair sweep ([#39](https://github.com/AtomiCloud/nitroso.tin/issues/39)) ([fb56231](https://github.com/AtomiCloud/nitroso.tin/commit/fb56231aa31797f0a881199a19f340c087838141)), closes [#35](https://github.com/AtomiCloud/nitroso.tin/issues/35) [#35](https://github.com/AtomiCloud/nitroso.tin/issues/35)

## [1.49.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.48.0...v1.49.0) (2026-07-09)


### ✨ Features ✨

* **cli:** add print-ticket command to re-download a ticket PDF from KTMB ([#38](https://github.com/AtomiCloud/nitroso.tin/issues/38)) ([98a32e5](https://github.com/AtomiCloud/nitroso.tin/commit/98a32e5985f656a5fd7dbf70e2d6770ce49f95c4))

## [1.48.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.47.0...v1.48.0) (2026-07-08)


### ✨ Features ✨

* **withdrawer:** 6-hourly reconcile sweep of Processing withdrawals ([91aa661](https://github.com/AtomiCloud/nitroso.tin/commit/91aa66120e6b0fb10cf04f9df4f522eafcee6a1b))

## [1.47.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.46.0...v1.47.0) (2026-07-08)


### 📜 Documentation 📜

* **helm:** regenerate root chart README for the withdrawer module ([c16fde8](https://github.com/AtomiCloud/nitroso.tin/commit/c16fde8a85b566db8df671aa18f27d3c2c7716f9))


### ✨ Features ✨

* **withdrawer:** nightly cron approving pending withdrawals via zinc ([8558777](https://github.com/AtomiCloud/nitroso.tin/commit/8558777d7ad1546d0eaddda7b45f14386e77099b))


### 🐛 Bug Fixes 🐛

* **withdrawer:** align approve path version segment with the SDK ([3884c8e](https://github.com/AtomiCloud/nitroso.tin/commit/3884c8e1b4ab44606c87c2c1fe703acffe20abb1))
* **withdrawer:** cap listing pages per sweep ([4b19e21](https://github.com/AtomiCloud/nitroso.tin/commit/4b19e2109a7091baca3c12f847dc23bacc5d516f))
* **withdrawer:** re-drive stuck Processing and harden the sweep ([f24ada5](https://github.com/AtomiCloud/nitroso.tin/commit/f24ada567e69b953a6a6648a05322239ca9bb69c))
* **helm:** sync Chart.lock after adding withdrawer dependency ([8f13594](https://github.com/AtomiCloud/nitroso.tin/commit/8f135941b821531dd8950439e2bdb227227cc96d)), closes [#31](https://github.com/AtomiCloud/nitroso.tin/issues/31)

## [1.46.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.45.0...v1.46.0) (2026-07-06)


### ✨ Features ✨

* **recoverer:** mark Duplicate when ticket not on our KTMB account ([#34](https://github.com/AtomiCloud/nitroso.tin/issues/34)) ([cd6a730](https://github.com/AtomiCloud/nitroso.tin/commit/cd6a730a25abfcf21b3d8ed05bd966af21a2f41e))

## [1.45.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.44.1...v1.45.0) (2026-07-05)


### ✨ Features ✨

* **buyer:** auto-revert to Pending on transient wallet-insufficient failure ([7ca7294](https://github.com/AtomiCloud/nitroso.tin/commit/7ca729436408db0917c9c9955a39333c7b22fe33))

## [1.44.1](https://github.com/AtomiCloud/nitroso.tin/compare/v1.44.0...v1.44.1) (2026-07-04)


### 🐛 Bug Fixes 🐛

* **buyer:** match KTMB's real duplicate-passport wording ([d0a504f](https://github.com/AtomiCloud/nitroso.tin/commit/d0a504f58329b1f3c1d2d91b8109383f5aa96617))

## [1.44.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.43.0...v1.44.0) (2026-07-03)


### 📜 Documentation 📜

* **recovery:** add argon frontend change surface to spec ([afe5a3c](https://github.com/AtomiCloud/nitroso.tin/commit/afe5a3c5556275b2ae7e7bc07b45a6c0ab4f6f83))
* **recovery:** add duplicate-passport recovery spec ([c5afa92](https://github.com/AtomiCloud/nitroso.tin/commit/c5afa92f6ff15907c030cb763e9fb90ad6cc286d))
* **recoverer:** document single-replica invariant ([df25dc4](https://github.com/AtomiCloud/nitroso.tin/commit/df25dc4ec8e26e33436a2fde99878e58aba33131))


### ✨ Features ✨

* **recoverer:** drain every 15m, sweep hourly ([1b6b493](https://github.com/AtomiCloud/nitroso.tin/commit/1b6b493643e05593e14191c4ed6d3ff0e0560cfb))
* **recoverer:** duplicate-passport recovery pipeline ([7fe25c2](https://github.com/AtomiCloud/nitroso.tin/commit/7fe25c29e36618fcd03f4e39017783a8a0e5b68d))


### 🐛 Bug Fixes 🐛

* **recoverer:** CI go-toolchain regression + round-2 money-safety ([4d58ffe](https://github.com/AtomiCloud/nitroso.tin/commit/4d58ffee2d037563d9012ad876824fb0fd68320d))
* **recoverer:** close sweep concurrency hole + deterministic re-scan complete ([3aa38ea](https://github.com/AtomiCloud/nitroso.tin/commit/3aa38eaa7592f3b6c99a1c79a5d6d6d3921590e2))
* **recoverer:** harden money-safety after review ([8c7bcb9](https://github.com/AtomiCloud/nitroso.tin/commit/8c7bcb96adc2ae1ef2ca04b4a30cd66037aee732))
* **recoverer:** harden scanner, buyer, and queue durability (multi-review pass) ([91a763e](https://github.com/AtomiCloud/nitroso.tin/commit/91a763e05fbdb9cca622e461afcac0437a1fc6d2))

## [1.43.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.42.0...v1.43.0) (2026-06-08)


### ✨ Features ✨

* reserver concurrency 100 + warm pool 50 ([98df34d](https://github.com/AtomiCloud/nitroso.tin/commit/98df34dffb28d2ec59066d94e7fbd8a67b4048c8))
* reserver concurrency 50 (matches warm pool 50) ([88e2980](https://github.com/AtomiCloud/nitroso.tin/commit/88e2980fc76b58fefe65a379b2c94814894e890c))
* warm KTMB connection pool + cached DNS for the reserver ([ffa38c1](https://github.com/AtomiCloud/nitroso.tin/commit/ffa38c1c83562cdd420f32bbdba669f7be7d3d4e))

## [1.42.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.41.1...v1.42.0) (2026-06-08)


### ✨ Features ✨

* add date/time/direction to reserve, buy, set-passenger failures ([c0bc40d](https://github.com/AtomiCloud/nitroso.tin/commit/c0bc40d09452fe0e918fe44a76317343fa2e718c))
* poll helium with a single mobile stream at 10ms delay ([32ee0b4](https://github.com/AtomiCloud/nitroso.tin/commit/32ee0b4458aa484341c7691fe781026bb5c559cc))

## [1.41.1](https://github.com/AtomiCloud/nitroso.tin/compare/v1.41.0...v1.41.1) (2026-06-08)


### 🐛 Bug Fixes 🐛

* make enricher resilient and incremental ([71d6d11](https://github.com/AtomiCloud/nitroso.tin/commit/71d6d11e9e13932be18a8c0c41a9a509729571e1))

## [1.41.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.40.0...v1.41.0) (2026-06-08)


### ✨ Features ✨

* helium web stateless and 120s poll duration ([a1b25bb](https://github.com/AtomiCloud/nitroso.tin/commit/a1b25bb090751802db38a8e39b5835b30e28de28))

## [1.40.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.39.0...v1.40.0) (2026-06-08)


### ✨ Features ✨

* route helium web streams through the proxy pool ([206ab2d](https://github.com/AtomiCloud/nitroso.tin/commit/206ab2deac283a0f9e76b279ce22426b820b48e4))


### 🚀 Performance Improvement 🚀

* reuse a single pooled HTTP client for KTMB calls ([ec1ce14](https://github.com/AtomiCloud/nitroso.tin/commit/ec1ce140866f265bca48194520e34f3303b4bea3))

## [1.39.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.38.0...v1.39.0) (2026-06-08)


### ✨ Features ✨

* trigger release ([cf78dbe](https://github.com/AtomiCloud/nitroso.tin/commit/cf78dbe7e18c9fa8197b72695080342e70bbe7e8))

## [1.38.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.37.0...v1.38.0) (2026-06-08)


### ✨ Features ✨

* cap poller to first 42 streams by date ([7b4072d](https://github.com/AtomiCloud/nitroso.tin/commit/7b4072d6efbe8814e3e993988146b3fa2812a7f5))


### 🐛 Bug Fixes 🐛

* cap helium job CPU limit at 1000m (1 core) ([dceb1c4](https://github.com/AtomiCloud/nitroso.tin/commit/dceb1c4c20ba12ef4b4fefedae4bcf76a8cf70e7))
* set helium job requests to 1/1Gi for Guaranteed QoS ([11dd8da](https://github.com/AtomiCloud/nitroso.tin/commit/11dd8da3f09cd4cd06ae40ffaf2fdd044d2e242d))

## [1.37.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.36.1...v1.37.0) (2026-06-08)


### ✨ Features ✨

* set helium poll delay to 25ms and shard to 15 streams/pod ([b741dd4](https://github.com/AtomiCloud/nitroso.tin/commit/b741dd45198a7113f696a4d6cf7fee7af52a4e50))
* userData pool loginer + helium multi-watch and sharding ([2546cba](https://github.com/AtomiCloud/nitroso.tin/commit/2546cba6c30d4c29f9dbc7730f5d398548b7ec4f))

## [1.36.1](https://github.com/AtomiCloud/nitroso.tin/compare/v1.36.0...v1.36.1) (2025-10-05)


### 🐛 Bug Fixes 🐛

* allow manual override ([a80894a](https://github.com/AtomiCloud/nitroso.tin/commit/a80894a5af870e566a16b8df54bb56fb09d0f231))
* bitnami legacy ([a0c6dce](https://github.com/AtomiCloud/nitroso.tin/commit/a0c6dceefcc1af44fb529f982dca80a4a59615ca))

## [1.36.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.35.0...v1.36.0) (2025-08-09)


### ✨ Features ✨

* allow better error printing for reserver ([e9190d1](https://github.com/AtomiCloud/nitroso.tin/commit/e9190d12103be744bb845a1e225929f7148b4ab0))

## [1.35.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.34.1...v1.35.0) (2025-08-03)


### ✨ Features ✨

* **eso:** use v1 instead of v1beta ([8f4514c](https://github.com/AtomiCloud/nitroso.tin/commit/8f4514c1a65a0ad8a9f2fe5dd8777ae398d2af49))


### 🔼 Dependency Upstreams 🔼

* ugprade nix flakes ([5a984cc](https://github.com/AtomiCloud/nitroso.tin/commit/5a984cc48d70a365a806650eb468d43445a75c5f))

## [1.34.1](https://github.com/AtomiCloud/nitroso.tin/compare/v1.34.0...v1.34.1) (2025-01-22)


### 🐛 Bug Fixes 🐛

* login error not propogating upwards ([fff845d](https://github.com/AtomiCloud/nitroso.tin/commit/fff845d4788a1e433c5ad81f24e4d6898b94c256))

## [1.34.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.33.4...v1.34.0) (2025-01-22)


### ✨ Features ✨

* force login key to share distributed key ([c8f3064](https://github.com/AtomiCloud/nitroso.tin/commit/c8f3064d0998800b497ccbd5a061bc36fd48dbd0))


### 🔼 Dependency Upstreams 🔼

* **nix:** update atomi version ([1a2b43c](https://github.com/AtomiCloud/nitroso.tin/commit/1a2b43cf859a9c668ccfc64bb28aea6778e5b761))

## [1.33.4](https://github.com/AtomiCloud/nitroso.tin/compare/v1.33.3...v1.33.4) (2025-01-08)


### 🐛 Bug Fixes 🐛

* attempt to use new cache ([8d5c078](https://github.com/AtomiCloud/nitroso.tin/commit/8d5c07898e4e25133440c74a4c724cbc9391c4d2))
* empty ([41f47f8](https://github.com/AtomiCloud/nitroso.tin/commit/41f47f8357ecab328b07e2f70363221a39c9e5d7))

## [1.33.3](https://github.com/AtomiCloud/nitroso.tin/compare/v1.33.2...v1.33.3) (2025-01-01)


### 🐛 Bug Fixes 🐛

* simplify docker build script for nscloud ([7fd187b](https://github.com/AtomiCloud/nitroso.tin/commit/7fd187b0dfd738f26e2a2d4542fc5e7876eea03d))

## [1.33.2](https://github.com/AtomiCloud/nitroso.tin/compare/v1.33.1...v1.33.2) (2024-12-30)


### 🐛 Bug Fixes 🐛

* incorrect prettier configuration ([846fd33](https://github.com/AtomiCloud/nitroso.tin/commit/846fd337e0a97f87695e72de1e34562399d3fdd5))
* incorrect secret pointing ([4fa572a](https://github.com/AtomiCloud/nitroso.tin/commit/4fa572accf526464c7cfe8716b67f0ff66e57066))
* incorrect semantic releaser configuration ([c7b79dc](https://github.com/AtomiCloud/nitroso.tin/commit/c7b79dc7be1d4b867f77c984ff1bd037430b55a5))

## [1.33.1](https://github.com/AtomiCloud/nitroso.tin/compare/v1.33.0...v1.33.1) (2024-08-12)


### 🐛 Bug Fixes 🐛

* use stream redis for terminator ([8b43060](https://github.com/AtomiCloud/nitroso.tin/commit/8b430608385f3a5c72b508b1faf8ddd02f8d6cd9))

## [1.33.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.32.0...v1.33.0) (2024-08-12)


### ✨ Features ✨

* log encrypted store published ([76a8180](https://github.com/AtomiCloud/nitroso.tin/commit/76a81806b2413f355e9e563411ab1f884675aa0d))


### 🐛 Bug Fixes 🐛

* incorrect store reference ([6b659ef](https://github.com/AtomiCloud/nitroso.tin/commit/6b659ef12b8a351ede5356041950d5586a6370b5))

## [1.32.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.31.1...v1.32.0) (2024-08-12)


### ✨ Features ✨

* log decryption process ([93f6749](https://github.com/AtomiCloud/nitroso.tin/commit/93f674995da417c5b29c74ce2875230095419383))

## [1.31.1](https://github.com/AtomiCloud/nitroso.tin/compare/v1.31.0...v1.31.1) (2024-08-12)


### 🐛 Bug Fixes 🐛

* increase deferred buy time up to 5 minutes ([c1449b1](https://github.com/AtomiCloud/nitroso.tin/commit/c1449b135665672d185e0aa20c13f188d05e598d))

## [1.31.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.30.2...v1.31.0) (2024-08-11)


### ✨ Features ✨

* pin to correct secret ref ([c0dc6a5](https://github.com/AtomiCloud/nitroso.tin/commit/c0dc6a5c1b635c37abcb73ed2ccfaa33fa6852c8))

## [1.30.2](https://github.com/AtomiCloud/nitroso.tin/compare/v1.30.1...v1.30.2) (2024-08-11)


### 🐛 Bug Fixes 🐛

* **revert:** non-all search ([c4ed511](https://github.com/AtomiCloud/nitroso.tin/commit/c4ed511c987a767de6105969ce13d7585a939dbc))

## [1.30.1](https://github.com/AtomiCloud/nitroso.tin/compare/v1.30.0...v1.30.1) (2024-08-11)


### 🐛 Bug Fixes 🐛

* manual key list ([9afa807](https://github.com/AtomiCloud/nitroso.tin/commit/9afa8072b2a3885d637f3f4f1fca33a2a85e42af))

## [1.30.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.29.2...v1.30.0) (2024-08-11)


### ✨ Features ✨

* upgrade to use infisical ([43c5958](https://github.com/AtomiCloud/nitroso.tin/commit/43c59584679d8a4a630488c288556dca9c8d01f0))

## [1.29.2](https://github.com/AtomiCloud/nitroso.tin/compare/v1.29.1...v1.29.2) (2024-08-08)


### 🐛 Bug Fixes 🐛

* allow role to get pods ([48aec66](https://github.com/AtomiCloud/nitroso.tin/commit/48aec663f22de18440f558dcfbb08807fe2c87ee))

## [1.29.1](https://github.com/AtomiCloud/nitroso.tin/compare/v1.29.0...v1.29.1) (2024-08-08)


### 🐛 Bug Fixes 🐛

* incorrect downward api for poller ([07295bd](https://github.com/AtomiCloud/nitroso.tin/commit/07295bda132429f8a4dffdf1e8dfbdb5c5e5d648))

## [1.29.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.28.0...v1.29.0) (2024-08-08)


### ✨ Features ✨

* create a single multi-watch job insteal of multiple watch jobs ([940c60b](https://github.com/AtomiCloud/nitroso.tin/commit/940c60bd38e04fb9aaa62c7da130e8726fc60914))

## [1.28.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.27.1...v1.28.0) (2024-08-08)


### ✨ Features ✨

* assign self as owner for jobs created ([c356910](https://github.com/AtomiCloud/nitroso.tin/commit/c35691091840155dc611e76d9e6fd5d8f89cbc87))

## [1.27.1](https://github.com/AtomiCloud/nitroso.tin/compare/v1.27.0...v1.27.1) (2024-08-04)


### 🐛 Bug Fixes 🐛

* reduce request ([18a8ce5](https://github.com/AtomiCloud/nitroso.tin/commit/18a8ce53913928e3cfa3b25096b352b74d32266b))

## [1.27.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.26.0...v1.27.0) (2024-08-04)


### ✨ Features ✨

* improve proxy-processing script ([d215dd2](https://github.com/AtomiCloud/nitroso.tin/commit/d215dd2e70bf639c0473331d64f9c91580428db9))

## [1.26.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.25.1...v1.26.0) (2024-07-13)


### ✨ Features ✨

* reduce ttl to 3minutes to reduce cluster clutter ([b9a1b52](https://github.com/AtomiCloud/nitroso.tin/commit/b9a1b5228e1881602ec9a8547c1160123bad4bb9))

## [1.25.1](https://github.com/AtomiCloud/nitroso.tin/compare/v1.25.0...v1.25.1) (2024-07-13)


### 🐛 Bug Fixes 🐛

* increase sleep buffer ([da80247](https://github.com/AtomiCloud/nitroso.tin/commit/da802476f632730c01cf36fe57b08de59d5ed297))


### 🔼 Dependency Upstreams 🔼

* update semantic-release to 0.9.0 ([3af0a96](https://github.com/AtomiCloud/nitroso.tin/commit/3af0a96152c1280b1bb22cdb8a7158245236eb8c))

## [1.25.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.24.0...v1.25.0) (2024-05-29)


### ✨ Features ✨

* disable proxy usage ([e56cb7f](https://github.com/AtomiCloud/nitroso.tin/commit/e56cb7f303ecd879dbee5f127de581d9849bfcbd))

## [1.24.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.23.0...v1.24.0) (2024-05-29)


### ✨ Features ✨

* increase delay to 16 seconds per time slot ([c9fa650](https://github.com/AtomiCloud/nitroso.tin/commit/c9fa650fbcb4dcd7e5453e2ec08c421a0af16b20))
* increase delay to 16 seconds per time slot ([3952dc5](https://github.com/AtomiCloud/nitroso.tin/commit/3952dc5e02eebcfa1ad40c50737558d771fcb1d7))

## [1.23.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.22.1...v1.23.0) (2024-05-29)


### ✨ Features ✨

* add delay between enricher polls ([60e1adc](https://github.com/AtomiCloud/nitroso.tin/commit/60e1adc89494467c55d2bdae38f50c7baa7c9bff))
* pin commit-analyzer ([53517a4](https://github.com/AtomiCloud/nitroso.tin/commit/53517a43bac0861231e973aafdc4f3156cab1529))


### 🐛 Bug Fixes 🐛

* incorrect atomi-release ([81aa1ec](https://github.com/AtomiCloud/nitroso.tin/commit/81aa1ecf53545c7b60ca03de98d8c9111a5a420b))
* incorrect pin ([8c982bf](https://github.com/AtomiCloud/nitroso.tin/commit/8c982bf25cd4e78d44c8f004c81c187dd9a4a39b))
* **attempt:** pin generator to 7.0.2 ([90dc94a](https://github.com/AtomiCloud/nitroso.tin/commit/90dc94a5239b5731cfdd356a706ecf0a8628e624))
* pin release-notes-generator ([46b6642](https://github.com/AtomiCloud/nitroso.tin/commit/46b6642d0c541d96f74e1cb1ba700b1917a252e8))
* pin to 7.0.2 ([b2a342c](https://github.com/AtomiCloud/nitroso.tin/commit/b2a342c61bab2a30927644d861d482441017df17))

## [1.22.1](https://github.com/AtomiCloud/nitroso.tin/compare/v1.22.0...v1.22.1) (2024-03-23)


### 🐛 Bug Fixes 🐛

* allow enricher to use proxy ([4319705](https://github.com/AtomiCloud/nitroso.tin/commit/43197059bd7c3ffe96d15d0a16cc7a982b0f85c7))

## [1.22.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.21.0...v1.22.0) (2024-03-23)


### ✨ Features ✨

* remove proxy from enricher & buyer and add delay in buyer ([2d5e0a1](https://github.com/AtomiCloud/nitroso.tin/commit/2d5e0a14d5eb08bf28f2b8e7baefe968034a14b3))
* use stream redis instead of upstash ([20e66c3](https://github.com/AtomiCloud/nitroso.tin/commit/20e66c3cf261c340aefd4afe6305dd13e5a9e2d4))

## [1.21.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.20.0...v1.21.0) (2024-03-17)


### ✨ Features ✨

* limit capabilites and add logs ([8791eb8](https://github.com/AtomiCloud/nitroso.tin/commit/8791eb884e7bc426b8595d6cffd6f9db54774caa))

## [1.20.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.19.2...v1.20.0) (2024-03-13)


### ✨ Features ✨

* logs for succesful passenger data purchase ([26b1908](https://github.com/AtomiCloud/nitroso.tin/commit/26b190870389431f9c5ddbf9a299c8318e01ef48))

## [1.19.2](https://github.com/AtomiCloud/nitroso.tin/compare/v1.19.1...v1.19.2) (2024-03-08)


### 🐛 Bug Fixes 🐛

* incorrect throttle placement ([fc87382](https://github.com/AtomiCloud/nitroso.tin/commit/fc8738262c785050737ce49ac1d5ee89d9450c07))

## [1.19.1](https://github.com/AtomiCloud/nitroso.tin/compare/v1.19.0...v1.19.1) (2024-03-08)


### 🐛 Bug Fixes 🐛

* throttle buyer ([24dbd7f](https://github.com/AtomiCloud/nitroso.tin/commit/24dbd7fc027512f51a23b7aa01a806800c154b77))

## [1.19.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.18.0...v1.19.0) (2024-03-08)


### ✨ Features ✨

* tweak concurrency values for better performance ([e7260a3](https://github.com/AtomiCloud/nitroso.tin/commit/e7260a3f1bb0d2475d092997f6f3de196f17e2a9))

## [1.18.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.17.1...v1.18.0) (2024-03-07)


### ✨ Features ✨

* allow configurable buffers ([1418453](https://github.com/AtomiCloud/nitroso.tin/commit/1418453d2f6c017873e2d9fd0709f82c982592b2))

## [1.17.1](https://github.com/AtomiCloud/nitroso.tin/compare/v1.17.0...v1.17.1) (2024-03-02)


### 🐛 Bug Fixes 🐛

* term signal not working ([065c6f6](https://github.com/AtomiCloud/nitroso.tin/commit/065c6f6cfd5a40a2546ef09e2b45e474655aea82))

## [1.17.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.16.0...v1.17.0) (2024-03-02)


### ✨ Features ✨

* reserver logs to more clearly indicate ticket targeting time ([615f905](https://github.com/AtomiCloud/nitroso.tin/commit/615f90517c84de5edc39dab1c79399ba063f992a))

## [1.16.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.15.0...v1.16.0) (2024-03-02)


### ✨ Features ✨

* dynamic concurrency and attempts inside and outside maintainence ([e5683c7](https://github.com/AtomiCloud/nitroso.tin/commit/e5683c7c2f32838d732632408d1544ac5d717d20))

## [1.15.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.14.0...v1.15.0) (2024-03-02)


### ✨ Features ✨

* emitted logs for term signal and added propogation for term signal ([0ffd85b](https://github.com/AtomiCloud/nitroso.tin/commit/0ffd85b21800def489a19464727cd34cacf9d82e))

## [1.14.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.13.1...v1.14.0) (2024-03-01)


### ✨ Features ✨

* terminator ([e8ad6af](https://github.com/AtomiCloud/nitroso.tin/commit/e8ad6afc56cf6a79b2bdd5067574f33de0378bbc))


### 🐛 Bug Fixes 🐛

* chart unsynced ([69919bf](https://github.com/AtomiCloud/nitroso.tin/commit/69919bf67eb6e1cc0fc876ad056db11df90dc2a9))
* incorrect values file ([6bd6325](https://github.com/AtomiCloud/nitroso.tin/commit/6bd6325edefa7297ace732afac59495895b2c7bf))

## [1.13.1](https://github.com/AtomiCloud/nitroso.tin/compare/v1.13.0...v1.13.1) (2024-02-28)


### 🐛 Bug Fixes 🐛

* remove logs about proxies ([ff1de70](https://github.com/AtomiCloud/nitroso.tin/commit/ff1de700694efe08bd61aa373f70ad6d32e86182))

## [1.13.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.12.1...v1.13.0) (2024-02-28)


### ✨ Features ✨

* release reverse sessions ([e28762e](https://github.com/AtomiCloud/nitroso.tin/commit/e28762e38db8abb2471d9a5e277f904e52bbc4d4))
* set ticket number and booking number in tickets ([7ed38e1](https://github.com/AtomiCloud/nitroso.tin/commit/7ed38e17ede15fa130862358a2165b0c1c2d6121))
* set ticket number and booking number in tickets ([c1116bd](https://github.com/AtomiCloud/nitroso.tin/commit/c1116bd3df28ec6835903ba05cff26c11f4673e1))


### 🐛 Bug Fixes 🐛

* compile errors due to change in SDK generator from swagger ([1e04ecc](https://github.com/AtomiCloud/nitroso.tin/commit/1e04eccc67fa4972639c586a49847ec6ba508740))

## [1.12.1](https://github.com/AtomiCloud/nitroso.tin/compare/v1.12.0...v1.12.1) (2024-02-28)


### 🐛 Bug Fixes 🐛

* bump release ([eaafe54](https://github.com/AtomiCloud/nitroso.tin/commit/eaafe5441b8007184c791de1d9ea3ecec951b408))

## [1.12.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.11.0...v1.12.0) (2024-02-27)


### ✨ Features ✨

* emulate rotating proxies ([f468b5b](https://github.com/AtomiCloud/nitroso.tin/commit/f468b5b79c655d574f9a83e468af15e39a2eed95))

## [1.11.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.10.0...v1.11.0) (2024-02-27)


### ✨ Features ✨

* allow random proxy ([d51df27](https://github.com/AtomiCloud/nitroso.tin/commit/d51df2708452564e84ee66910d71174ce271deae))


### 🐛 Bug Fixes 🐛

* terminate other local replicas when successfully booked ([794a421](https://github.com/AtomiCloud/nitroso.tin/commit/794a421d05d3135e29476a572e8ed503b911cb5a))

## [1.10.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.9.2...v1.10.0) (2024-02-27)


### ✨ Features ✨

* convert reserver to StatefulSet with stream group name as pod name ([6bc7fbb](https://github.com/AtomiCloud/nitroso.tin/commit/6bc7fbb4fd05416166b468143df376e9b687a0f7))

## [1.9.2](https://github.com/AtomiCloud/nitroso.tin/compare/v1.9.1...v1.9.2) (2024-02-26)


### 🐛 Bug Fixes 🐛

* reduce replicas to 3 ([a2fdf0f](https://github.com/AtomiCloud/nitroso.tin/commit/a2fdf0f54c588b7dd43492a6620d14a81f2c60b3))

## [1.9.1](https://github.com/AtomiCloud/nitroso.tin/compare/v1.9.0...v1.9.1) (2024-02-26)


### 🐛 Bug Fixes 🐛

* reduce resource request ([cced82f](https://github.com/AtomiCloud/nitroso.tin/commit/cced82ffece893cc453acde90b4505874a98ad65))

## [1.9.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.8.0...v1.9.0) (2024-02-26)


### ✨ Features ✨

* upgrade pollee to 1.9.2 ([fa049d6](https://github.com/AtomiCloud/nitroso.tin/commit/fa049d6e842a310588922c4bcedce6a1eea58c87))

## [1.8.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.7.1...v1.8.0) (2024-02-24)


### ✨ Features ✨

* update helium pointer ([3510ff3](https://github.com/AtomiCloud/nitroso.tin/commit/3510ff3ddce20f33a91c122d956f89354ff574c0))

## [1.7.1](https://github.com/AtomiCloud/nitroso.tin/compare/v1.7.0...v1.7.1) (2024-02-24)


### 🐛 Bug Fixes 🐛

* test empty message ([a7a8714](https://github.com/AtomiCloud/nitroso.tin/commit/a7a87140a61e980f3d350e0d8783168c3ad93e10))

## [1.7.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.6.0...v1.7.0) (2024-02-24)


### ✨ Features ✨

* migrate to new actions ([a255102](https://github.com/AtomiCloud/nitroso.tin/commit/a255102262788277ed15e0a853cb6a2c154a1874))


### 🐛 Bug Fixes 🐛

* incorrect docker setup action ([e17468d](https://github.com/AtomiCloud/nitroso.tin/commit/e17468dcadeb6131dcbfa0fe7c57d13c3a47d672))

## [1.6.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.5.4...v1.6.0) (2024-02-23)


### ✨ Features ✨

* incorrect proxy setting ([599c0df](https://github.com/AtomiCloud/nitroso.tin/commit/599c0dfcc1229eed368ee6680f55dbee8f4ed0a1))
* incorrect proxy setting ([ee2124d](https://github.com/AtomiCloud/nitroso.tin/commit/ee2124de2ea3414e2d2ff4ae19da4a717e8bddde))


### 🐛 Bug Fixes 🐛

* incorrect token for CI ([7af26ea](https://github.com/AtomiCloud/nitroso.tin/commit/7af26ea5a9c240b8e812f9164162bb5f5cd3a6d7))
* release with npm ([7b372a5](https://github.com/AtomiCloud/nitroso.tin/commit/7b372a5407ab71e3444f7f1d3b5867b716e52ed8))

## [1.5.4](https://github.com/AtomiCloud/nitroso.tin/compare/v1.5.3...v1.5.4) (2024-02-03)


### 🐛 Bug Fixes 🐛

* OTEL non compatible schema ([3b0d981](https://github.com/AtomiCloud/nitroso.tin/commit/3b0d9811d9543b4eeb32ce6db1ccd64e338bfdbe))

## [1.5.3](https://github.com/AtomiCloud/nitroso.tin/compare/v1.5.2...v1.5.3) (2024-02-02)


### 🐛 Bug Fixes 🐛

* missing settings for nitroso ([37a423a](https://github.com/AtomiCloud/nitroso.tin/commit/37a423a1134aabfa8c5486511093ba4706db03c9))

## [1.5.2](https://github.com/AtomiCloud/nitroso.tin/compare/v1.5.1...v1.5.2) (2024-02-01)


### 🐛 Bug Fixes 🐛

* missing proxy ([d693d89](https://github.com/AtomiCloud/nitroso.tin/commit/d693d896d6f89543f4156656bfc1ea70b201db3d))

## [1.5.1](https://github.com/AtomiCloud/nitroso.tin/compare/v1.5.0...v1.5.1) (2024-01-31)


### 🐛 Bug Fixes 🐛

* missing proxy for App ([f3d0f4c](https://github.com/AtomiCloud/nitroso.tin/commit/f3d0f4c2bf226d7118e2ef35c1f119abe98c2859))

## [1.5.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.4.13...v1.5.0) (2024-01-31)


### ✨ Features ✨

* allow for proxy ([fa03c6e](https://github.com/AtomiCloud/nitroso.tin/commit/fa03c6ebe10e22b048238ceb9943b22a16504428))

## [1.4.13](https://github.com/AtomiCloud/nitroso.tin/compare/v1.4.12...v1.4.13) (2024-01-30)


### 🐛 Bug Fixes 🐛

* attempt to have test ([954a65f](https://github.com/AtomiCloud/nitroso.tin/commit/954a65fefd9a988d229b6268db2a63026fde430b))

## [1.4.12](https://github.com/AtomiCloud/nitroso.tin/compare/v1.4.11...v1.4.12) (2024-01-04)


### 🐛 Bug Fixes 🐛

* get count does not account for changing time ([18c402b](https://github.com/AtomiCloud/nitroso.tin/commit/18c402b7a1fcedfcc459ecc1f3585d09f68ac158))

## [1.4.11](https://github.com/AtomiCloud/nitroso.tin/compare/v1.4.10...v1.4.11) (2024-01-03)


### 🐛 Bug Fixes 🐛

* incorrect within range check ([fe508d6](https://github.com/AtomiCloud/nitroso.tin/commit/fe508d604606797134a517ee0bfbe9bf25f18496))

## [1.4.10](https://github.com/AtomiCloud/nitroso.tin/compare/v1.4.9...v1.4.10) (2024-01-03)


### 🐛 Bug Fixes 🐛

* incorrect timing range check ([1a1fbc8](https://github.com/AtomiCloud/nitroso.tin/commit/1a1fbc835e022ee83d565469605cf13246b2d81d))

## [1.4.9](https://github.com/AtomiCloud/nitroso.tin/compare/v1.4.8...v1.4.9) (2024-01-02)


### 🐛 Bug Fixes 🐛

* check where did count go wrong ([11c1007](https://github.com/AtomiCloud/nitroso.tin/commit/11c1007bb8289a29e290b637ceb6eb515e5deb78))

## [1.4.8](https://github.com/AtomiCloud/nitroso.tin/compare/v1.4.7...v1.4.8) (2024-01-02)


### 🐛 Bug Fixes 🐛

* connect tin-enricher re-propogation ([8a1b94a](https://github.com/AtomiCloud/nitroso.tin/commit/8a1b94a2398a104aa18cc43233d01f93bc48d3c8))
* update pichu to use luna account ([e402d22](https://github.com/AtomiCloud/nitroso.tin/commit/e402d22a616e46c3f6c9a5db6c3be5af719e18b2))

## [1.4.7](https://github.com/AtomiCloud/nitroso.tin/compare/v1.4.6...v1.4.7) (2024-01-02)


### 🐛 Bug Fixes 🐛

* remove debug statement at start ([66f2f20](https://github.com/AtomiCloud/nitroso.tin/commit/66f2f20f22b5733bb50e510816f463b4871a7b08))

## [1.4.6](https://github.com/AtomiCloud/nitroso.tin/compare/v1.4.5...v1.4.6) (2024-01-02)


### 🐛 Bug Fixes 🐛

* missing configuration sync ([5b0fa76](https://github.com/AtomiCloud/nitroso.tin/commit/5b0fa766b53a5e90666f18e2f439ecc6241c2fde))
* run config sync before pushing ([ed8d4ae](https://github.com/AtomiCloud/nitroso.tin/commit/ed8d4aec3fea5caa0910e2bab05fe81a44d8970d))

## [1.4.5](https://github.com/AtomiCloud/nitroso.tin/compare/v1.4.4...v1.4.5) (2024-01-02)


### 🐛 Bug Fixes 🐛

* migrating away from upstash for streams ([fa47369](https://github.com/AtomiCloud/nitroso.tin/commit/fa47369136a38410b60f05a41249d522eea8dafc))

## [1.4.4](https://github.com/AtomiCloud/nitroso.tin/compare/v1.4.3...v1.4.4) (2024-01-01)


### 🐛 Bug Fixes 🐛

* psm and ps configuration problems ([4819e22](https://github.com/AtomiCloud/nitroso.tin/commit/4819e22ba8435ff997e9902608c8e895a6892b1f))

## [1.4.3](https://github.com/AtomiCloud/nitroso.tin/compare/v1.4.2...v1.4.3) (2024-01-01)


### 🐛 Bug Fixes 🐛

* app module in values ([7d8fed3](https://github.com/AtomiCloud/nitroso.tin/commit/7d8fed316d89b89aebaf7a915006ec844ddaa15b))

## [1.4.2](https://github.com/AtomiCloud/nitroso.tin/compare/v1.4.1...v1.4.2) (2024-01-01)


### 🐛 Bug Fixes 🐛

* tracer behaviour ([f5a9709](https://github.com/AtomiCloud/nitroso.tin/commit/f5a970990f777ad6e75c8d2104cebd997a0287b0))

## [1.4.1](https://github.com/AtomiCloud/nitroso.tin/compare/v1.4.0...v1.4.1) (2024-01-01)


### 🐛 Bug Fixes 🐛

* incorrect OTLP module ([46007da](https://github.com/AtomiCloud/nitroso.tin/commit/46007da98134a3a4db0043766a684ca703e397df))

## [1.4.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.3.4...v1.4.0) (2023-12-27)


### ✨ Features ✨

* release arm images ([da0c9f7](https://github.com/AtomiCloud/nitroso.tin/commit/da0c9f7142a29c13e7a1e21701af2a9fc3453030))

## [1.3.4](https://github.com/AtomiCloud/nitroso.tin/compare/v1.3.3...v1.3.4) (2023-12-27)


### 🐛 Bug Fixes 🐛

* trigger semantic-releaser ([799830d](https://github.com/AtomiCloud/nitroso.tin/commit/799830d6bf375f2d8e20246d29af03a30adb4f12))

## [1.3.3](https://github.com/AtomiCloud/nitroso.tin/compare/v1.3.2...v1.3.3) (2023-12-27)


### 🐛 Bug Fixes 🐛

* missing sync config ([cbb2279](https://github.com/AtomiCloud/nitroso.tin/commit/cbb2279cd59f65b84a965e272aa9a512e8570f67))

## [1.3.2](https://github.com/AtomiCloud/nitroso.tin/compare/v1.3.1...v1.3.2) (2023-12-25)


### 🐛 Bug Fixes 🐛

* increase to 100 replicas ([de6431f](https://github.com/AtomiCloud/nitroso.tin/commit/de6431f222bfa29a66ea27811c7c0892fa1cee05))

## [1.3.1](https://github.com/AtomiCloud/nitroso.tin/compare/v1.3.0...v1.3.1) (2023-12-24)


### 🐛 Bug Fixes 🐛

* reduce cpu and ram usage for buyer ([03f53d1](https://github.com/AtomiCloud/nitroso.tin/commit/03f53d1af6faf264b044fc595a23924ff3f1dce6))

## [1.3.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.2.5...v1.3.0) (2023-12-24)


### ✨ Features ✨

* increase system usage ([f2daab7](https://github.com/AtomiCloud/nitroso.tin/commit/f2daab75108cec12670b95dc70a5a4f2040ad799))

## [1.2.5](https://github.com/AtomiCloud/nitroso.tin/compare/v1.2.4...v1.2.5) (2023-12-24)


### 🐛 Bug Fixes 🐛

* incorrect ca cert and tzdata ([5082c60](https://github.com/AtomiCloud/nitroso.tin/commit/5082c609f7fbad7913a73261ea6f8092855c5996))

## [1.2.4](https://github.com/AtomiCloud/nitroso.tin/compare/v1.2.3...v1.2.4) (2023-12-21)


### 🐛 Bug Fixes 🐛

* incorrect descope ID ([4b578b5](https://github.com/AtomiCloud/nitroso.tin/commit/4b578b5cf4f00ddb6cc424e22ef9cc3c77f61834))

## [1.2.3](https://github.com/AtomiCloud/nitroso.tin/compare/v1.2.2...v1.2.3) (2023-12-21)


### 🐛 Bug Fixes 🐛

* incorrect livecache endpoint ([f664fd6](https://github.com/AtomiCloud/nitroso.tin/commit/f664fd6222cfe6b48ecfa58deb74f440655d10ab))
* upstream helium to 1.4.5 ([7914b0e](https://github.com/AtomiCloud/nitroso.tin/commit/7914b0e1a6ffa62f3589a1f7173490debf68de08))

## [1.2.2](https://github.com/AtomiCloud/nitroso.tin/compare/v1.2.1...v1.2.2) (2023-12-21)


### 🐛 Bug Fixes 🐛

* upstream helium to 1.4.5 ([2c559c0](https://github.com/AtomiCloud/nitroso.tin/commit/2c559c02922c0c3335bf1f2250a022d03f929be6))

## [1.2.1](https://github.com/AtomiCloud/nitroso.tin/compare/v1.2.0...v1.2.1) (2023-12-20)


### 🐛 Bug Fixes 🐛

* pollee config oncce and for all ([4663c7f](https://github.com/AtomiCloud/nitroso.tin/commit/4663c7fdddbd7e2517eb0ab51bcc515506cc2c24))

## [1.2.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.1.7...v1.2.0) (2023-12-20)


### ✨ Features ✨

* ttl for jobs ([a2d82e9](https://github.com/AtomiCloud/nitroso.tin/commit/a2d82e9b61ceb06269042350629eda219789152a))


### 🐛 Bug Fixes 🐛

* pollee config not propogated ([e803c9d](https://github.com/AtomiCloud/nitroso.tin/commit/e803c9d6ad35ede17427a0703503ec47be2094e0))

## [1.1.7](https://github.com/AtomiCloud/nitroso.tin/compare/v1.1.6...v1.1.7) (2023-12-20)


### 🐛 Bug Fixes 🐛

* incorrect job configuration ([2bd9ad7](https://github.com/AtomiCloud/nitroso.tin/commit/2bd9ad7b6b8018352b2e7453e5ac13a5bdf14446))

## [1.1.6](https://github.com/AtomiCloud/nitroso.tin/compare/v1.1.5...v1.1.6) (2023-12-20)


### 🐛 Bug Fixes 🐛

* attempt solve CA issue ([4c08fbf](https://github.com/AtomiCloud/nitroso.tin/commit/4c08fbf8ab910e61a48d56d2645301da6e61da7b))

## [1.1.5](https://github.com/AtomiCloud/nitroso.tin/compare/v1.1.4...v1.1.5) (2023-12-20)


### 🐛 Bug Fixes 🐛

* use upstash recommend way for connecting to redis ([ba4f9af](https://github.com/AtomiCloud/nitroso.tin/commit/ba4f9af86c2ca69a3d6a011a0ddfff71fbfe24e8))

## [1.1.4](https://github.com/AtomiCloud/nitroso.tin/compare/v1.1.3...v1.1.4) (2023-12-20)


### 🐛 Bug Fixes 🐛

* missing ca-certificates ([2837129](https://github.com/AtomiCloud/nitroso.tin/commit/2837129e7e311f2ce9558b27435f75664f91ee33))

## [1.1.3](https://github.com/AtomiCloud/nitroso.tin/compare/v1.1.2...v1.1.3) (2023-12-20)


### 🐛 Bug Fixes 🐛

* missing tzdata ([644ad20](https://github.com/AtomiCloud/nitroso.tin/commit/644ad20f8f85456469ae1e5e8954e7e1ae4ef211))

## [1.1.2](https://github.com/AtomiCloud/nitroso.tin/compare/v1.1.1...v1.1.2) (2023-12-20)


### 🐛 Bug Fixes 🐛

* dragonfly cannot use annotation, migrate to redis ([f44249f](https://github.com/AtomiCloud/nitroso.tin/commit/f44249f18aeeeefd12c583919f49674c849e4fb6))
* incorrect CI yaml ([f5cddf2](https://github.com/AtomiCloud/nitroso.tin/commit/f5cddf20b92849bf744230e35a8f70865da983eb))
* remove linux arm ([bff6cbc](https://github.com/AtomiCloud/nitroso.tin/commit/bff6cbc0f08376c215c1c41a9a7aeffa6c77dfa2))
* upgrade helium version and use master cache endpoint ([68f70c7](https://github.com/AtomiCloud/nitroso.tin/commit/68f70c71adb18688c922a46c3de5db9942420112))
* use arm64 arch ([86a04db](https://github.com/AtomiCloud/nitroso.tin/commit/86a04dbb45f2e60276497584dd28047fbdc0befb))

## [1.1.1](https://github.com/AtomiCloud/nitroso.tin/compare/v1.1.0...v1.1.1) (2023-12-20)


### 🐛 Bug Fixes 🐛

* missing annotations ([1adf382](https://github.com/AtomiCloud/nitroso.tin/commit/1adf3821f2540d1cb1272d5488cc1261cc588984))

## [1.1.0](https://github.com/AtomiCloud/nitroso.tin/compare/v1.0.0...v1.1.0) (2023-12-20)


### ✨ Features ✨

* configure pichu, pikachu, raichu ([7cb0593](https://github.com/AtomiCloud/nitroso.tin/commit/7cb05930d9f54ed3c046b271a58549843c3e83ce))

## 1.0.0 (2023-12-20)


### ✨ Features ✨

* initial commit ([9e935ed](https://github.com/AtomiCloud/nitroso.tin/commit/9e935edcfc3d59ab52b9d9617f471bc39c2dc360))


### 🐛 Bug Fixes 🐛

* golang-ci lint for github actions ([d91efd0](https://github.com/AtomiCloud/nitroso.tin/commit/d91efd050887fd91b232cac223e110219aeff9ad))
