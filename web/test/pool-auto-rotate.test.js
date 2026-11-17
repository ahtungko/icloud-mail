import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";
import test from "node:test";

const source = readFileSync(new URL("../src/views/PoolView.vue", import.meta.url), "utf8").replace(/\r\n/g, "\n");
const handler = source.match(/async function openMemberAutoRotate\(row\) \{[\s\S]*?\n\}/)[0];
const cleanup = source.match(/onBeforeUnmount\(\(\) => \{\n[\s\S]*?\n\}\);/)[0];

function setup() {
  const pending = new Map();
  const state = {
    autoRotateOpen: { value: false },
    autoRotateAlias: { value: null },
    error: { value: null },
    ElMessage: { warning() { assert.fail("stale response must not show warnings"); } },
    fetchMemberAlias(id) {
      return new Promise((resolve, reject) => pending.set(id, { resolve, reject }));
    },
    onBeforeUnmount(fn) { state.unmount = fn; },
  };
  vm.createContext(state);
  vm.runInContext(`
    let autoRotateReadTicket = 0;
    let poolRefreshTicket = 0, memberReadTicket = 0, leaseReadTicket = 0, sharedReadTicket = 0;
    let poolRefreshController, sharedReadController;
    ${handler}
    ${cleanup}
  `, state);
  return { state, pending };
}

test("late responses cannot replace the last selected rotation mailbox", async () => {
  const { state, pending } = setup();
  const first = state.openMemberAutoRotate({ alias_id: 1 });
  const second = state.openMemberAutoRotate({ alias_id: 2 });
  pending.get(2).resolve({ id: 2, credentialMode: "v2" });
  await second;
  pending.get(1).resolve({ id: 1, credentialMode: "v2" });
  await first;
  assert.equal(state.autoRotateAlias.value.id, 2);
  assert.equal(state.autoRotateOpen.value, true);
});

test("stale request errors are ignored while current errors remain visible", async () => {
  const { state, pending } = setup();
  const first = state.openMemberAutoRotate({ alias_id: 1 });
  const second = state.openMemberAutoRotate({ alias_id: 2 });
  pending.get(1).reject(new Error("stale failure"));
  await first;
  assert.equal(state.error.value, null);
  const failure = new Error("current failure");
  pending.get(2).reject(failure);
  await second;
  assert.equal(state.error.value, failure);
});

test("leaving the page prevents pending requests from opening a dialog", async () => {
  const { state, pending } = setup();
  const request = state.openMemberAutoRotate({ alias_id: 1 });
  state.unmount();
  pending.get(1).resolve({ id: 1, credentialMode: "v2" });
  await request;
  assert.equal(state.autoRotateOpen.value, false);
  assert.equal(state.autoRotateAlias.value, null);
});
