# Changelog

## [0.2.10](https://github.com/pavelpascari/sdf/compare/v0.2.9...v0.2.10) (2026-02-28)


### Bug Fixes

* **ci:** pass -R flag to gh workflow run in release-please ([#129](https://github.com/pavelpascari/sdf/issues/129)) ([7939594](https://github.com/pavelpascari/sdf/commit/7939594c92a3c4c5b55d4b45d19d630a320d5fc1))

## [0.2.9](https://github.com/pavelpascari/sdf/compare/v0.2.8...v0.2.9) (2026-02-28)


### Bug Fixes

* **ci:** use GitHub App token for release-please ([#127](https://github.com/pavelpascari/sdf/issues/127)) ([154a30b](https://github.com/pavelpascari/sdf/commit/154a30bcd1ee9cb6d351fe5c2e14d8555e4de211))

## [0.2.8](https://github.com/pavelpascari/sdf/compare/v0.2.7...v0.2.8) (2026-02-28)


### Features

* add smart shell autocompletion for all commands ([#97](https://github.com/pavelpascari/sdf/issues/97)) ([679cd70](https://github.com/pavelpascari/sdf/commit/679cd70fa505dccfaa147b1d4d877dd9bf971964))


### Bug Fixes

* **ci:** trigger goreleaser from release-please workflow ([#125](https://github.com/pavelpascari/sdf/issues/125)) ([e91135a](https://github.com/pavelpascari/sdf/commit/e91135a8931ddafef1f6defb8c7b21ff7df4b890))

## [0.2.7](https://github.com/pavelpascari/sdf/compare/v0.2.6...v0.2.7) (2026-02-28)


### Features

* add --json flag for structured sync output ([#104](https://github.com/pavelpascari/sdf/issues/104)) ([08eed74](https://github.com/pavelpascari/sdf/commit/08eed74f5d1e856ea066b6b05382ff44e44511da))
* add render package with event bus, TTY/JSON renderers, and ANSI helpers ([#98](https://github.com/pavelpascari/sdf/issues/98)) ([2e017d8](https://github.com/pavelpascari/sdf/commit/2e017d803b8ded0fce3a12ef5f538b950a5e7338))
* **branch:** add --json flag for structured output ([#110](https://github.com/pavelpascari/sdf/issues/110)) ([713ddad](https://github.com/pavelpascari/sdf/commit/713ddadad774c06045a2d3606d7a67822e692c1c))
* **config:** add --json flag for structured output ([#118](https://github.com/pavelpascari/sdf/issues/118)) ([18d719d](https://github.com/pavelpascari/sdf/commit/18d719d9c9a67509533375486672f5b16df74300))
* **doctor:** add --json flag for structured output ([#116](https://github.com/pavelpascari/sdf/issues/116)) ([f9d2d12](https://github.com/pavelpascari/sdf/commit/f9d2d122f2e370d9f1a2d807d0dfb769d2f026f2))
* event-driven bus with print/warn/err and batch lifecycle ([#101](https://github.com/pavelpascari/sdf/issues/101)) ([e094624](https://github.com/pavelpascari/sdf/commit/e094624d1f6f15437ee33b970758c33f4a3a0473))
* **fetch:** add --json flag for structured output ([#112](https://github.com/pavelpascari/sdf/issues/112)) ([3e1dbd0](https://github.com/pavelpascari/sdf/commit/3e1dbd0327c4c99ccc878f693cc06fa87ba5eb13))
* **merge:** add --json flag for structured output ([#108](https://github.com/pavelpascari/sdf/issues/108)) ([d50427c](https://github.com/pavelpascari/sdf/commit/d50427c26217c4ded63c95e2e660f13838241343))
* **move:** add --json flag for structured output ([#120](https://github.com/pavelpascari/sdf/issues/120)) ([6701585](https://github.com/pavelpascari/sdf/commit/670158552d94b32cd09b8f828c6df14b8e9d7b45))
* **rdr:** mergeability ([#123](https://github.com/pavelpascari/sdf/issues/123)) ([8d24344](https://github.com/pavelpascari/sdf/commit/8d24344ef75e17efb5dfdc662a39e1094507a1cc))
* **status:** add --json flag for structured output ([#106](https://github.com/pavelpascari/sdf/issues/106)) ([5517b83](https://github.com/pavelpascari/sdf/commit/5517b838d96faff64bef1b3521bbb4b5da8697e9))
* **status:** display CI check status for each PR ([#121](https://github.com/pavelpascari/sdf/issues/121)) ([599068c](https://github.com/pavelpascari/sdf/commit/599068cdd4ae32ff67bda721d43e0a90b6f38a02))
* **switch:** add --json flag for structured output ([#114](https://github.com/pavelpascari/sdf/issues/114)) ([7afd479](https://github.com/pavelpascari/sdf/commit/7afd479a9c9b24e4c141dd8e432f4dd3341bb31a))


### Bug Fixes

* **ci:** fix YAML syntax error in release-checklist workflow ([#124](https://github.com/pavelpascari/sdf/issues/124)) ([0c36926](https://github.com/pavelpascari/sdf/commit/0c36926e4776aa538e21c717b093e9bc776fce70))


### Code Refactoring

* **branch:** route output through render.Bus ([#109](https://github.com/pavelpascari/sdf/issues/109)) ([c517018](https://github.com/pavelpascari/sdf/commit/c5170187f937791349e3d12412196e54ad0d2087))
* **config:** route all output through render.Bus ([#117](https://github.com/pavelpascari/sdf/issues/117)) ([7f530be](https://github.com/pavelpascari/sdf/commit/7f530be3e8ed6227604828b6aac1bf21c45a74d4))
* **doctor:** route all output through render.Bus ([#115](https://github.com/pavelpascari/sdf/issues/115)) ([05777ef](https://github.com/pavelpascari/sdf/commit/05777ef35bc43f147f858009e90392fbd68eb145))
* **fetch:** route all output through render.Bus ([#111](https://github.com/pavelpascari/sdf/issues/111)) ([0d22095](https://github.com/pavelpascari/sdf/commit/0d220953270a963abec475a641a556dc1cf28710))
* **merge:** route all output through render.Bus ([#107](https://github.com/pavelpascari/sdf/issues/107)) ([dda8195](https://github.com/pavelpascari/sdf/commit/dda81954083bb5e3b0a6199b8bcae2940d768ae8))
* **move:** route all output through render.Bus ([#119](https://github.com/pavelpascari/sdf/issues/119)) ([36219b3](https://github.com/pavelpascari/sdf/commit/36219b3d3947af8b14a9ca8650fb693128d68d04))
* route all sync and prnav output through render.Bus ([#99](https://github.com/pavelpascari/sdf/issues/99)) ([68f8a73](https://github.com/pavelpascari/sdf/commit/68f8a735fca2d42e266385e03e29f73671b6bfde))
* **status:** route all output through render.Bus ([#105](https://github.com/pavelpascari/sdf/issues/105)) ([b75b9dd](https://github.com/pavelpascari/sdf/commit/b75b9dd3c552f0ced1ddb9d87f5fe9bf70478079))
* **switch:** route all output through render.Bus ([#113](https://github.com/pavelpascari/sdf/issues/113)) ([9f01762](https://github.com/pavelpascari/sdf/commit/9f017621ce44eb241034e9108caaf66b45dbae4e))


### Documentation

* add render package research, design, and implementation plan ([#100](https://github.com/pavelpascari/sdf/issues/100)) ([5fc6dee](https://github.com/pavelpascari/sdf/commit/5fc6dee769b2ef64e4b39d6eda34bc8ef23a4faf))

## [0.2.6](https://github.com/pavelpascari/sdf/compare/v0.2.5...v0.2.6) (2026-02-26)


### Miscellaneous

* correct changelogs for v0.2.1 through v0.2.5 ([#94](https://github.com/pavelpascari/sdf/issues/94)) ([f51c45e](https://github.com/pavelpascari/sdf/commit/f51c45efc80de912b25949bdd5f5b5528e737bb2))

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
