import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { effectScope, reactive, ref, watch } from "vue";

const componentPath = new URL(
  "../src/components/AutoRotateDialog.vue",
  import.meta.url,
);

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

async function sourceText() {
  return readFile(componentPath, "utf8");
}

function mountScript(source, initial = {}) {
  const script = source.match(/<script setup>([\s\S]*?)<\/script>/)?.[1];
  assert.ok(script, "component has a script setup block");
  const body = script.replace(/^import .*;\s*$/gm, "");
  const props = reactive({
    modelValue: true,
    alias: { id: 1, address: "one@example.test", autoRotateDurationMinutes: 0 },
    csrfToken: "csrf-token",
    ...initial,
  });
  const events = [];
  const messages = [];
  const requests = [];
  const unmountCallbacks = [];
  const scope = effectScope();
  const api = (id, minutes, token) => {
    const request = deferred();
    requests.push({ id, minutes, token, ...request });
    return request.promise;
  };
  const globals = {
    ref,
    watch,
    defineProps: () => props,
    defineEmits: () => (...args) => events.push(args),
    onBeforeUnmount: (callback) => unmountCallbacks.push(callback),
    ElMessage: {
      success: (message) => messages.push(["success", message]),
      error: (message) => messages.push(["error", message]),
    },
    setAliasAutoRotate: (...args) => api(...args),
  };
  const exposed = scope.run(() => Function(
    ...Object.keys(globals),
    `${body}\nreturn { selectedOption, customMinutes, saving, submitSave, submitDisable, updateVisibility };`,
  )(...Object.values(globals)));
  return {
    ...exposed,
    props,
    events,
    messages,
    requests,
    scope,
    unmount: () => {
      for (const callback of unmountCallbacks) callback();
      scope.stop();
    },
  };
}

test("cancel and reopen for the same alias restores the saved draft", async () => {
  const source = await sourceText();
  const vm = mountScript(source, {
    alias: { id: 1, address: "one@example.test", autoRotateDurationMinutes: 45 },
  });
  assert.equal(vm.selectedOption.value, "custom");
  assert.equal(vm.customMinutes.value, 45);
  vm.updateVisibility(false);
  assert.deepEqual(vm.events.pop(), ["update:modelValue", false]);
  vm.customMinutes.value = 99;
  vm.selectedOption.value = "custom";
  vm.props.modelValue = false;
  vm.props.modelValue = true;
  assert.equal(vm.selectedOption.value, "custom");
  assert.equal(vm.customMinutes.value, 45);
  vm.unmount();
});

test("custom duration accepts only whole minutes from 1 through 10080", async () => {
  const source = await sourceText();
  for (const invalid of [null, undefined, "", 0, -1, 1.5, 10081]) {
    const vm = mountScript(source);
    vm.selectedOption.value = "custom";
    vm.customMinutes.value = invalid;
    await vm.submitSave();
    assert.equal(vm.requests.length, 0, `invalid custom value ${String(invalid)} called API`);
    assert.equal(vm.saving.value, false);
    vm.unmount();
  }
});

test("preset, custom, explicit zero, and disable requests preserve their values", async () => {
  const source = await sourceText();
  for (const [option, custom, expected] of [[30, 71, 30], [60, 71, 60], [120, 71, 120], ["custom", 71, 71], [0, 71, 0]]) {
    const vm = mountScript(source);
    vm.selectedOption.value = option;
    vm.customMinutes.value = custom;
    const saving = vm.submitSave();
    assert.equal(vm.requests[0].minutes, expected);
    vm.requests[0].resolve({ alias: { id: 1 } });
    await saving;
    assert.equal(vm.saving.value, false);
    vm.unmount();
  }
  for (const minutes of [1, 10080]) {
    const vm = mountScript(source);
    vm.selectedOption.value = "custom";
    vm.customMinutes.value = minutes;
    const saving = vm.submitSave();
    assert.equal(vm.requests[0].minutes, minutes);
    vm.requests[0].resolve({ alias: { id: 1 } });
    await saving;
    vm.unmount();
  }
  const vm = mountScript(source, {
    alias: { id: 7, address: "seven@example.test", autoRotateDurationMinutes: 30 },
  });
  const disabling = vm.submitDisable();
  assert.equal(vm.requests[0].id, 7);
  assert.equal(vm.requests[0].minutes, 0);
  vm.requests[0].resolve({ alias: { id: 7 } });
  await disabling;
  vm.unmount();
});

test("duplicate submit is ignored while a request is pending", async () => {
  const vm = mountScript(await sourceText());
  const first = vm.submitSave();
  await vm.submitSave();
  assert.equal(vm.requests.length, 1);
  vm.requests[0].resolve({ alias: { id: 1 } });
  await first;
  vm.unmount();
});

test("stale completions after close, reopen, or alias switch do not affect the new session", async () => {
  const source = await sourceText();
  for (const change of [
    (vm) => { vm.props.modelValue = false; vm.props.modelValue = true; },
    (vm) => { vm.props.alias = { id: 2, address: "two@example.test", autoRotateDurationMinutes: 60 }; },
  ]) {
    const vm = mountScript(source);
    const first = vm.submitSave();
    change(vm);
    vm.selectedOption.value = 120;
    const second = vm.submitSave();
    assert.equal(vm.requests.length, 2);
    const eventCount = vm.events.length;
    vm.requests[0].resolve({ alias: { id: 1 } });
    await first;
    assert.equal(vm.events.length, eventCount);
    assert.equal(vm.saving.value, true, "stale completion leaves the new request pending");
    vm.requests[1].resolve({ alias: { id: 2 } });
    await second;
    vm.unmount();
  }
  const vm = mountScript(source);
  const first = vm.submitSave();
  vm.props.modelValue = false;
  vm.props.modelValue = true;
  const eventCount = vm.events.length;
  vm.requests[0].reject(new Error("stale failure"));
  await first;
  assert.equal(vm.events.length, eventCount);
  assert.equal(vm.messages.length, 0);
  assert.equal(vm.saving.value, false);
  vm.unmount();

  const unmounted = mountScript(source);
  const pendingAfterUnmount = unmounted.submitSave();
  unmounted.unmount();
  unmounted.requests[0].resolve({ alias: { id: 1 } });
  await pendingAfterUnmount;
  assert.deepEqual(unmounted.events, []);
  assert.equal(unmounted.saving.value, true, "unmount invalidates the request without mutating state");
});

test("failed save reports error and can be retried", async () => {
  const vm = mountScript(await sourceText());
  const first = vm.submitSave();
  vm.requests[0].reject(new Error("network down"));
  await first;
  assert.deepEqual(vm.messages[0], ["error", "network down"]);
  assert.equal(vm.saving.value, false);
  const retry = vm.submitSave();
  assert.equal(vm.requests.length, 2);
  vm.requests[1].resolve({ alias: { id: 1 } });
  await retry;
  assert.equal(vm.events.some(([name]) => name === "updated"), true);
  vm.unmount();
});

test("dialog cannot close through its chrome or escape while saving", async () => {
  const source = await sourceText();
  const template = source.slice(source.indexOf("<template>"), source.indexOf("<script setup>"));
  assert.match(template, /:show-close="!saving"/);
  assert.match(template, /:close-on-press-escape="!saving"/);
  assert.match(template, /:close-on-click-modal="false"/);
  assert.match(template, /:disabled="saving"/);
  assert.match(template, /@update:model-value="updateVisibility"/);
  const vm = mountScript(source);
  const pending = vm.submitSave();
  assert.equal(vm.saving.value, true);
  vm.updateVisibility(false);
  assert.equal(vm.events.length, 0, "a close request during save is ignored");
  vm.requests[0].resolve({ alias: { id: 1 } });
  await pending;
  vm.unmount();
});
