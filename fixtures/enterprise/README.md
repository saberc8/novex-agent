# Enterprise retrieval fixtures

All companies, people, prices, policies and documents here are synthetic test data. The uploaded knowledge fixtures belong to tenant **SC001**. A second tenant has deliberately conflicting database values so a same-name SKU or warehouse cannot silently cross the tenant boundary.

The two workbooks were authored with the bundled `@oai/artifact-tool`. They preserve original XLSX files, numeric and date cells, multiple sheets, Excel tables and saved formula values. They are not Markdown exports or vector/RAG chunks.

| File | Contents | Document access |
| --- | --- | --- |
| `hardware-catalog-50000.xlsx` | 2,500 SKUs × 20 monthly price snapshots, 50,000 Catalog rows; 8 AfterSales rules | SC001 tenant, published by `scale_admin` |
| `hardware-procurement-private.xlsx` | 24 Procurement rows with 48 cached formula results; 4 SupplierTerms rules | Only uploader `scale_c` |
| `md/KB-HR-*.md`, `KB-WMS-*.md`, `KB-AS-*.md`, `KB-PRODUCT-*.md` | 64 HR, warehouse, aftersales and product policy/FAQ documents | SC001 tenant |
| `md/KB-PROC-*.md` | 16 procurement policy/FAQ documents | Only uploader `scale_c` |

`manifest.json` records each file's hash, intended scope, owner, worksheet dimensions and deterministic reference facts. Public Catalog records contain SC001 historical sales prices only. Database row/field grants do **not** narrow an uploaded workbook: document access applies to every worksheet and cell in that workbook.

Useful independent checks:

- `Catalog!A50000:I50000` is SKU `HW-002499`, month `2026-11`, price **888.25**. Its previous monthly row is `Catalog!A47500:I47500`, price **888.15**.
- `Procurement!C2=38`, `D2=120`, `E2=0.13`; `F2=ROUND(C2*(1+E2),2)` has cached **42.94** and `G2=ROUND(D2*F2,2)` has cached **5152.80**. Changing quantity to 121 during authoring was checked to produce **5195.74**, then restored before export.
- Catalog dates cover April 2025 through November 2026; the original date cells are typed dates. Warranty is 12 months for all fixture products.
- Each Markdown document contains a stable KB identifier, rule, FAQ, version and evidence/access note.

The checked-in XLSX files are ready to upload and require no authoring SDK. The following optional original authoring workflow requires Codex's bundled runtime; it is not a dependency of the running Demo. With that environment available, regenerate from the repository root:

```sh
node --max-old-space-size=12288 scripts/enterprise/build_fixtures.mjs
```

The runtime must resolve `@oai/artifact-tool`; `scripts/enterprise/node_modules` can link to the dependency directory returned by Codex's `load_workspace_dependencies`. The builder renders every sheet, scans formula errors, tests recalculation and writes bounded QA records into `.local/enterprise-artifacts/`. Exporter debug sidecars are removed; the two XLSX deliverables total about 2 MiB.

After scale databases and account registration finish, prepare the live application:

```sh
python3 scripts/check_enterprise.py --prepare
python3 scripts/check_enterprise.py --oracle-only --output .local/enterprise-oracles.json
python3 scripts/check_enterprise.py --run
```

Preparation uploads or reuses all 82 fixtures, verifies hashes and checks visible counts: A/B 65, C 82, X 0. Preparation changes the policy epoch; finish every account, grant and document mutation before starting regression conversations.

The default regression runs **16 real Agent turns**, split evenly between Pi and DeepSeek Harness, sequentially. Each Agent turn may make multiple model/tool requests internally. Three conversation groups exercise follow-up salary denial, refreshed inventory/shipping checks, and historical Excel comparison. Other cases cover department payroll, tenant identity, annual leave, incomplete aggregation, live aftersales, private formula results, private-workbook denial, unknown SKU, and order/outbound evidence.

Numeric answers are extracted from actual assistant JSON and compared using `Decimal` with current authorized API query results. Workbook answers require a successful spreadsheet-query audit and sheet/row citations. The report retains complete answers, prompts, expected facts, reference query results, audit records, status changes, elapsed time and failure reasons. It does not simulate an Agent or treat a few matching keywords as numeric correctness. Natural-language nuance still needs review; backend audit proves resource/document access but does not expose the Agent's full internal reasoning or tool arguments.

`--only excel_late_row` reruns its whole multi-turn group. Use a separate `--output` path for retries to retain the original evidence. All logins use `BOOTSTRAP_ADMIN_PASSWORD` from local configuration; scripts never print the password or model credentials.

The business API accepts both a resource UUID and its registered key. Audits preserve whichever identifier the caller used; the evaluator resolves only explicit UUID/key pairs in the registration manifest and still requires a successful query by the tested account. `--reevaluate INPUT --output DIFFERENT_OUTPUT` rechecks saved real evidence with the current evaluator, retains the previous assessment, and makes no new Agent calls.
