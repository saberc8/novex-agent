# Third-party sources and licenses

Novex Agent's project license is [Apache-2.0](LICENSE). Third-party source
attributions and dependency licenses remain applicable.

## Template and admin frontend

- The Go/RBAC scaffold was imported from [saberc8/template-rbac-go](https://github.com/saberc8/template-rbac-go), itself derived from [saberc8/avalon-template-ex](https://github.com/saberc8/avalon-template-ex). Those template repositories did not contain a root license in the imported snapshot; this publication adds the Novex project license rather than claiming to preserve a nonexistent template license.
- The `admin/` frontend derives from [ContiNew Admin UI v3.7.0](https://github.com/continew-org/continew-admin-ui/tree/v3.7.0), licensed under Apache-2.0. Its original license and `Copyright (c) 2022-present Charles7c Authors. All Rights Reserved.` attribution are retained in [admin/LICENSE](admin/LICENSE).
- Novex adds management pages and API bindings under `admin/src/views/novex/` and `admin/src/apis/novex/`, updates package scripts/metadata, and changes the production endpoint configuration to the deployed origin. Existing upstream attribution is retained.
- ContiNew credits [Gi Demo](https://gitee.com/lin0716/gi-demo) as an upstream UI source. This acknowledgement preserves that provenance; it does not assert a separately verified license for Gi Demo.

## External Agent runtimes and editor

| Package | Pinned/declared version | License information |
|---|---|---|
| `@earendil-works/pi-coding-agent` | 1.5.0 | MIT, as declared by the locked package |
| `@deepseek-ai/dsh` | 0.2.2-rc.1 | MIT |
| `@deepseek-ai/dsh-sdk-client` | 0.2.2-rc.1 | MIT |
| `aieditor` | installed 1.4.2; see admin lockfile | Its package contains the GNU Lesser General Public License, version 2.1 |

These packages are installed through their package managers; `node_modules/`
and Agent runtime binaries are not vendored in this source repository. Their
licenses and distribution obligations are independent of Novex's Apache-2.0
license. In particular, AIEditor's LGPL terms are not replaced by Apache-2.0.
This table highlights relevant components rather than serving as an exhaustive
dependency license inventory; consult all npm/pnpm and Go dependency metadata
when redistributing built applications or runtime images.

## Synthetic fixtures

Files under `fixtures/` are synthetic test records, policies and workbooks
provided with this project. The checked-in XLSX files can be used directly.
Their optional original authoring script uses Codex's bundled
`@oai/artifact-tool`, which is not included or required to run Novex Agent.

The configured model service is external. The project license does not grant
access to a model API, model weights, paid service, or provider credentials.
