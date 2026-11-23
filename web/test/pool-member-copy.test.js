import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";
import test from "node:test";
import { buildAliasReceiveLink } from "../src/utils/aliasExport.js";

const source = readFileSync(new URL("../src/views/PoolView.vue", import.meta.url), "utf8").replace(/\r\n/g, "\n");
const handler = source.match(/async function copyMemberOTP\(row\) \{[\s\S]*?\n\}/)[0];

test("pool credentials display current, legacy and absent receive links", async () => {
  const showCredentials = source.match(/async function showCredentials\(id\) \{[\s\S]*?\n\}/)[0];
  const origin = "https://admin.example.test";
  const legacyPath = "/api/v1/mail/recent?api_key=legacy";
  const otpPath = "/api/v1/otp?token=current";
  let alias, shown;
  const state = {
    location: { origin },
    buildAliasReceiveLink,
    attempt: (action) => action(),
    request: async () => ({ alias }),
    reveal: (_, credentials) => { shown = credentials; },
  };
  vm.runInNewContext(showCredentials, state);
  for (const [paths, expected] of [
    [{ otp_url_path: otpPath, direct_link_path: legacyPath }, origin + otpPath],
    [{ otp_url_path: legacyPath, direct_link_path: legacyPath }, origin + legacyPath],
    [{ direct_link_path: legacyPath }, origin + legacyPath],
    [{}, ""],
  ]) {
    alias = { address: "mail@example.test", ...paths };
    await state.showCredentials(1);
    assert.equal(shown.otp_url, expected);
    assert.equal(shown.email, alias.address);
  }
});

function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

function setup() {
  const reads = [], copies = [], messages = [];
  const state = {
    copyingMemberOTP: { value: false },
    error: { value: null },
    fetchMemberAlias(id) {
      const pending = deferred();
      reads.push({ id, ...pending });
      return pending.promise;
    },
    buildAliasExportLine(alias) { return `link:${alias.id}`; },
    copyText(text) {
      const pending = deferred();
      copies.push({ text, ...pending });
      return pending.promise;
    },
    ElMessage: { success(message) { messages.push(message); } },
  };
  vm.createContext(state);
  vm.runInContext(handler, state);
  return { state, reads, copies, messages };
}

test("member copy excludes other rows through both fetch and clipboard", async () => {
  const { state, reads, copies, messages } = setup();
  const first = state.copyMemberOTP({ alias_id: 1 });
  assert.equal(state.copyingMemberOTP.value, true);
  await state.copyMemberOTP({ alias_id: 2 });
  assert.deepEqual(reads.map(read => read.id), [1]);

  reads[0].resolve({ id: 1 });
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(copies[0].text, "link:1");
  assert.equal(state.copyingMemberOTP.value, true);
  await state.copyMemberOTP({ alias_id: 2 });
  assert.equal(reads.length, 1);
  copies[0].resolve(true);
  await first;
  assert.equal(state.copyingMemberOTP.value, false);
  assert.equal(messages.length, 1);
  assert.equal(state.error.value, null);

  const next = state.copyMemberOTP({ alias_id: 2 });
  assert.equal(reads.length, 2);
  reads[1].resolve({ id: 2 });
  await new Promise(resolve => setImmediate(resolve));
  copies[1].resolve(true);
  await next;
});

test("fetch and clipboard failures release the lock for a retry", async () => {
  const { state, reads, copies, messages } = setup();
  const failure = new Error("fetch failed");
  const first = state.copyMemberOTP({ alias_id: 1 });
  reads[0].reject(failure);
  await first;
  assert.equal(state.error.value, failure);
  assert.equal(state.copyingMemberOTP.value, false);

  const second = state.copyMemberOTP({ alias_id: 2 });
  assert.equal(state.error.value, null);
  reads[1].resolve({ id: 2 });
  await new Promise(resolve => setImmediate(resolve));
  copies[0].resolve(false);
  await second;
  assert.match(state.error.value.message, /复制失败/);
  assert.equal(state.copyingMemberOTP.value, false);
  assert.equal(messages.length, 0);

  const retry = state.copyMemberOTP({ alias_id: 3 });
  reads[2].resolve({ id: 3 });
  await new Promise(resolve => setImmediate(resolve));
  copies[1].resolve(true);
  await retry;
  assert.equal(state.error.value, null);
  assert.equal(state.copyingMemberOTP.value, false);
  assert.equal(messages.length, 1);
});
