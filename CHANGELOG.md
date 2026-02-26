# Changelog

## [0.2.5](https://github.com/pavelpascari/sdf/compare/v0.2.4...v0.2.5) (2026-02-26)


### Features

* add sdf split command for AI-powered branch decomposition ([#64](https://github.com/pavelpascari/sdf/issues/64)) ([c019bad](https://github.com/pavelpascari/sdf/commit/c019badc3b757c36bd5f2860ca400527e78b420e))
* add split execution engine with tree validation and cleanup ([#63](https://github.com/pavelpascari/sdf/issues/63)) ([15c0d15](https://github.com/pavelpascari/sdf/commit/15c0d159a39f0084c4eb3ae7739b62e6d8bba839))
* add claude-powered two-phase split analysis with hunk assignment ([#62](https://github.com/pavelpascari/sdf/issues/62)) ([8a67cfd](https://github.com/pavelpascari/sdf/commit/8a67cfd077417f678a10c5c654077d36f7001af7))
* add unified diff parser with hunk filtering and formatting ([#61](https://github.com/pavelpascari/sdf/issues/61)) ([72efafa](https://github.com/pavelpascari/sdf/commit/72efafa2e801212aac5463d2cd822599b60cc093))
* add split plan types, YAML parsing, and validation ([#60](https://github.com/pavelpascari/sdf/issues/60)) ([ba9c1aa](https://github.com/pavelpascari/sdf/commit/ba9c1aa5f94727703bb0421b5b70eb2ea76fe829))
* add git diff/patch helpers and extend claude wrapper with streaming session support ([#59](https://github.com/pavelpascari/sdf/issues/59)) ([82336fa](https://github.com/pavelpascari/sdf/commit/82336fafa2c0372be569194352479bfcfe34d709))


### Documentation

* add design docs and implementation plans for hunk-level decomposition and plan refinement ([#65](https://github.com/pavelpascari/sdf/issues/65)) ([ff9e088](https://github.com/pavelpascari/sdf/commit/ff9e08801e03f4ed9919c5fe659b962988993382))
* add initial design docs and implementation plans for sdf split ([#57](https://github.com/pavelpascari/sdf/issues/57)) ([9c4f0bf](https://github.com/pavelpascari/sdf/commit/9c4f0bf5d34bf80a8043650049259f8c4ff9e8f2))


### CI/CD

* add changelog sections for all conventional commit types ([#90](https://github.com/pavelpascari/sdf/issues/90)) ([8dd36fe](https://github.com/pavelpascari/sdf/commit/8dd36fe326ea373447393faa25739cf5f6870fe0))
* move golangci-lint from pre-push to pre-commit hook ([#91](https://github.com/pavelpascari/sdf/issues/91)) ([ed53335](https://github.com/pavelpascari/sdf/commit/ed5333512e4182fd01eee3bc2da10eecff6e8497))


### Miscellaneous

* update build config, gitignore, and reduce property test trials ([#58](https://github.com/pavelpascari/sdf/issues/58)) ([f2e3e82](https://github.com/pavelpascari/sdf/commit/f2e3e824114f2ab6538edb0e8585fed3814a5576))

## [0.2.4](https://github.com/pavelpascari/sdf/compare/v0.2.3...v0.2.4) (2026-02-26)


### Features

* rename `sdf init` to `sdf new` with backward-compatible alias ([#85](https://github.com/pavelpascari/sdf/issues/85)) ([58147a5](https://github.com/pavelpascari/sdf/commit/58147a5724f953c1223cfba41cb78abbb8471047))


### Bug Fixes

* pass prompt via stdin in RunPromptStreamingWithOpts ([#83](https://github.com/pavelpascari/sdf/issues/83)) ([975782b](https://github.com/pavelpascari/sdf/commit/975782b5e8f60ca5e35e217b9554f0193a4540c3))


### Code Refactoring

* replace generic e2e test names with meaningful real-world scenarios ([#56](https://github.com/pavelpascari/sdf/issues/56)) ([71ef8c7](https://github.com/pavelpascari/sdf/commit/71ef8c7ebd0ec79daa8aa895eee26bcf44e2d557))

## [0.2.3](https://github.com/pavelpascari/sdf/compare/v0.2.2...v0.2.3) (2026-02-25)


### Features

* add sdf ai intro command ([#37](https://github.com/pavelpascari/sdf/issues/37)) ([559e94e](https://github.com/pavelpascari/sdf/commit/559e94e55382db5e27c87e94801f389cb12d5cea))

## [0.2.2](https://github.com/pavelpascari/sdf/compare/v0.2.1...v0.2.2) (2026-02-25)


### Features

* add version check to notify users of newer releases ([#54](https://github.com/pavelpascari/sdf/issues/54)) ([4f54e75](https://github.com/pavelpascari/sdf/commit/4f54e751d744099265f8dd237df274eedb17f005))
* conventional commits by default with scope from branch prefix ([#79](https://github.com/pavelpascari/sdf/issues/79)) ([a7b8121](https://github.com/pavelpascari/sdf/commit/a7b812160edc8287f62870e4d7be542aee23164c))


### Bug Fixes

* correct spelling of customized in comments ([#80](https://github.com/pavelpascari/sdf/issues/80)) ([aa1848d](https://github.com/pavelpascari/sdf/commit/aa1848d83642cb89a726ed257b9dd47bec99b013))
* prevent sdf init and register from overwriting existing config ([#75](https://github.com/pavelpascari/sdf/issues/75)) ([70fcea3](https://github.com/pavelpascari/sdf/commit/70fcea33ff3e4c80285e5f299566116680f8bff6))


### Code Refactoring

* remove sdf context feature references ([#53](https://github.com/pavelpascari/sdf/issues/53)) ([b27be3f](https://github.com/pavelpascari/sdf/commit/b27be3f0085668261760fd2040837b5e27e1f9e1))
* isolate spy recording behind spyrecord build tag ([#38](https://github.com/pavelpascari/sdf/issues/38)) ([f6af021](https://github.com/pavelpascari/sdf/commit/f6af02102ea94374b44367487dd5eaf7cdc711ff))


### Documentation

* add blog post draft: testing CLI tools that shell out ([#43](https://github.com/pavelpascari/sdf/issues/43)) ([43f81b1](https://github.com/pavelpascari/sdf/commit/43f81b1b91b5f907a5385f772bceb7dd02f40c9e))
* add comprehensive testing and validation plan ([#36](https://github.com/pavelpascari/sdf/issues/36)) ([2b44f78](https://github.com/pavelpascari/sdf/commit/2b44f7864eb7c5a3eaf9cbcf97f97a7592b4d059))


### CI/CD

* add release-please for automated versioning and changelog ([#45](https://github.com/pavelpascari/sdf/issues/45)) ([31b1b53](https://github.com/pavelpascari/sdf/commit/31b1b5334d3c6654ae2c9c414b25c1690b1740bc))
* add security scanning, code quality linting, and dependency management ([#44](https://github.com/pavelpascari/sdf/issues/44)) ([618a6af](https://github.com/pavelpascari/sdf/commit/618a6af8c8b8887541c9223b8cb272895e40f699))
* add workflow to auto-merge green dependabot PRs ([#51](https://github.com/pavelpascari/sdf/issues/51)) ([9996a41](https://github.com/pavelpascari/sdf/commit/9996a4166cb1d9cf4a84f6a151e8e573b409f5b9))
* bump actions/checkout from 4 to 6 ([#49](https://github.com/pavelpascari/sdf/issues/49)) ([4c741dd](https://github.com/pavelpascari/sdf/commit/4c741dd5f9eca034fd2d41ef9400812f34dfbc0f))
* bump actions/setup-go from 5 to 6 ([#50](https://github.com/pavelpascari/sdf/issues/50)) ([581c23e](https://github.com/pavelpascari/sdf/commit/581c23e864c019dfddd4b67f2ecccec0b92f63ae))
* bump github/codeql-action from 3 to 4 ([#47](https://github.com/pavelpascari/sdf/issues/47)) ([7f08e46](https://github.com/pavelpascari/sdf/commit/7f08e46ffbb4efb41fe70d9663ccbe04c794c955))
* bump goreleaser/goreleaser-action from 6 to 7 ([#48](https://github.com/pavelpascari/sdf/issues/48)) ([2666d8f](https://github.com/pavelpascari/sdf/commit/2666d8fdbf0d6ca2c309c470463b0fa1e28cb74e))
* skip blog post check for dependabot-only releases ([#55](https://github.com/pavelpascari/sdf/issues/55)) ([fa73cf4](https://github.com/pavelpascari/sdf/commit/fa73cf4cb19e5c6f6aa62f5bddfbda0f3e207d5d))


### Build System

* bump github.com/spf13/pflag from 1.0.9 to 1.0.10 ([#46](https://github.com/pavelpascari/sdf/issues/46)) ([99227bf](https://github.com/pavelpascari/sdf/commit/99227bf0040d592e0ae79262a0c0d61ae2a9a631))

## [0.2.1](https://github.com/pavelpascari/sdf/compare/v0.2.0...v0.2.1) (2026-02-25)


### CI/CD

* add blog feature and release checklist enforcement ([#34](https://github.com/pavelpascari/sdf/issues/34)) ([cc95029](https://github.com/pavelpascari/sdf/commit/cc95029892a667875f41a936965b93e086fbe56a))
* skip release checklist for patch versions ([#35](https://github.com/pavelpascari/sdf/issues/35)) ([3079353](https://github.com/pavelpascari/sdf/commit/3079353c52f524a89fbe7c6ea3a973a5d38a49a1))
