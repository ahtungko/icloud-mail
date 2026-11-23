import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";
import test from "node:test";

const source = readFileSync(new URL("../src/views/AccountDetailView.vue", import.meta.url), "utf8");
const handlers = ["deleteSelectedAliases", "submitAliasImportDelete"].map(name =>
  source.match(new RegExp("async function " + name + "\\([\\s\\S]*?\\n}"))[0]).join("\n");

for (const cancelAt of [1, 2, 0]) {
  test(`pasted deletion preserves manual selection (cancel at confirmation ${cancelAt})`, async () => {
    const selected = new Map([[1, { id: 1 }]]);
    const submitted = [];
    let confirmations = 0;
    const state = {
      selectedAliasRecords: { value: selected }, selectedAliasIds: { get value() { return [...state.selectedAliasRecords.value.keys()]; } },
      aliasImportDeleteAccountKey: { value: "account" }, detailRouteKey: () => "account",
      aliasImportDeletePreview: { value: { matched: [{ id: 2 }] } }, aliasImportDeleteLoading: { value: false },
      aliasImportDeleteVisible: { value: true }, viewActive: true, isCustomMailbox: { value: false }, loading: { value: false },
      auth: { state: { username: "admin", csrfToken: "csrf" } }, detailMutationPending: () => false,
      deletionController: { submit: async ids => submitted.push(Array.from(ids)) },
      deletionState: { value: { blocked: false } }, batchDeleteConfirming: { value: false },
      detailRequestKey: () => "current", beginDetailMutation() {}, loadDetail() {},
      ElMessageBox: { async confirm() { if (++confirmations === cancelAt) throw "cancel"; return "confirm"; } },
      confirmationCancelled: e => e === "cancel", showRequestError: assert.fail,
    };
    vm.runInNewContext(handlers, state);
    await state.submitAliasImportDelete();
    assert.equal(state.selectedAliasRecords.value, selected);
    assert.deepEqual(submitted, cancelAt ? [] : [[2]]);
    await state.deleteSelectedAliases();
    assert.deepEqual(submitted.at(-1), [1]);
  });
}
