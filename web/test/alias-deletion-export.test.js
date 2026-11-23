import assert from "node:assert/strict";
import test from "node:test";
import { buildAliasDeletionCsv } from "../src/utils/aliasDeletionExport.js";

test("deletion CSV separates remote status from local retention and escapes formulas", () => {
  const csv = buildAliasDeletionCsv({ jobId: "job-1", results: [
    { address: "=danger@example.com", deleted: false, code: "BATCH_DELETE_INTERRUPTED", message: "待核查" },
    { address: "ambiguous@example.com", deleted: false, localRetained: true, code: "APPLE_UPSTREAM_ERROR" },
    { address: "ok@example.com", deleted: true },
    { address: "waiting@example.com", deleted: false, code: "BATCH_DELETE_NOT_STARTED" },
    { address: "deferred@example.com", deleted: false, code: "APPLE_BATCH_DEFERRED", localRetained: true },
  ] });
  assert.match(csv, /"'=danger@example.com","待核查","未知"/);
  assert.match(csv, /"ambiguous@example.com","待核查","已保留"/);
  assert.match(csv, /"deferred@example.com","未执行","已保留"/);
  assert.match(csv, /已删除/);
  assert.match(csv, /未执行/);
  assert.match(csv, /已保留/);
  assert.match(csv, /"waiting@example.com","未执行"/);
  assert.equal(csv.split("\r\n").length, 7);
});
