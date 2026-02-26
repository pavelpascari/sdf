# Changelog

## [0.2.5](https://github.com/pavelpascari/sdf/compare/v0.2.4...v0.2.5) (2026-02-26)


### CI/CD

* add changelog sections for all conventional commit types ([#90](https://github.com/pavelpascari/sdf/issues/90)) ([8dd36fe](https://github.com/pavelpascari/sdf/commit/8dd36fe326ea373447393faa25739cf5f6870fe0))
* move golangci-lint from pre-push to pre-commit hook ([#91](https://github.com/pavelpascari/sdf/issues/91)) ([ed53335](https://github.com/pavelpascari/sdf/commit/ed5333512e4182fd01eee3bc2da10eecff6e8497))

## [0.2.4](https://github.com/pavelpascari/sdf/compare/v0.2.3...v0.2.4) (2026-02-26)


### Features

* rename `sdf init` to `sdf new` with backward-compatible alias ([#85](https://github.com/pavelpascari/sdf/issues/85)) ([58147a5](https://github.com/pavelpascari/sdf/commit/58147a5724f953c1223cfba41cb78abbb8471047))


### Bug Fixes

* pass prompt via stdin in RunPromptStreamingWithOpts ([#83](https://github.com/pavelpascari/sdf/issues/83)) ([975782b](https://github.com/pavelpascari/sdf/commit/975782b5e8f60ca5e35e217b9554f0193a4540c3))

## [0.2.3](https://github.com/pavelpascari/sdf/compare/v0.2.2...v0.2.3) (2026-02-25)


### Features

* add sdf ai intro command ([#37](https://github.com/pavelpascari/sdf/issues/37)) ([559e94e](https://github.com/pavelpascari/sdf/commit/559e94e55382db5e27c87e94801f389cb12d5cea))

## [0.2.2](https://github.com/pavelpascari/sdf/compare/v0.2.1...v0.2.2) (2026-02-25)


### Features

* add lightweight reconciliation during sync PR poll ([#29](https://github.com/pavelpascari/sdf/issues/29)) ([a3be3c9](https://github.com/pavelpascari/sdf/commit/a3be3c9a93f7a1030232159ad0332e0c5b0416cb))
* add pr title generation, conventional commits, and json output ([#22](https://github.com/pavelpascari/sdf/issues/22)) ([569248a](https://github.com/pavelpascari/sdf/commit/569248a0e8436f7dd66358e780dc0340d9b0b4ed))
* add sdf merge command for safe one-at-a-time PR merging ([#27](https://github.com/pavelpascari/sdf/issues/27)) ([b62d2aa](https://github.com/pavelpascari/sdf/commit/b62d2aaac19347c3986b32cda5e33b77d4632d24))
* add stack reconciliation with sdf fetch command ([#32](https://github.com/pavelpascari/sdf/issues/32)) ([e7b9279](https://github.com/pavelpascari/sdf/commit/e7b92791049d3dacf0c15d6088533c72ca00c6fd))
* add version check to notify users of newer releases ([#54](https://github.com/pavelpascari/sdf/issues/54)) ([4f54e75](https://github.com/pavelpascari/sdf/commit/4f54e751d744099265f8dd237df274eedb17f005))
* conventional commits by default with scope from branch prefix ([#79](https://github.com/pavelpascari/sdf/issues/79)) ([a7b8121](https://github.com/pavelpascari/sdf/commit/a7b812160edc8287f62870e4d7be542aee23164c))
* insert branches at current checkout position ([#30](https://github.com/pavelpascari/sdf/issues/30)) ([a25cafb](https://github.com/pavelpascari/sdf/commit/a25cafb4d9830e35ceccfd8ae226f443f195bd7b))


### Bug Fixes

* correct spelling of customized in comments ([#80](https://github.com/pavelpascari/sdf/issues/80)) ([aa1848d](https://github.com/pavelpascari/sdf/commit/aa1848d83642cb89a726ed257b9dd47bec99b013))
* prevent sdf init and register from overwriting existing config ([#75](https://github.com/pavelpascari/sdf/issues/75)) ([70fcea3](https://github.com/pavelpascari/sdf/commit/70fcea33ff3e4c80285e5f299566116680f8bff6))

## [0.2.1](https://github.com/pavelpascari/sdf/compare/v0.2.0...v0.2.1) (2026-02-25)


### Features

* add version check to notify users of newer releases ([#54](https://github.com/pavelpascari/sdf/issues/54)) ([4f54e75](https://github.com/pavelpascari/sdf/commit/4f54e751d744099265f8dd237df274eedb17f005))
* conventional commits by default with scope from branch prefix ([#79](https://github.com/pavelpascari/sdf/issues/79)) ([a7b8121](https://github.com/pavelpascari/sdf/commit/a7b812160edc8287f62870e4d7be542aee23164c))


### Bug Fixes

* correct spelling of customized in comments ([#80](https://github.com/pavelpascari/sdf/issues/80)) ([aa1848d](https://github.com/pavelpascari/sdf/commit/aa1848d83642cb89a726ed257b9dd47bec99b013))
* prevent sdf init and register from overwriting existing config ([#75](https://github.com/pavelpascari/sdf/issues/75)) ([70fcea3](https://github.com/pavelpascari/sdf/commit/70fcea33ff3e4c80285e5f299566116680f8bff6))
