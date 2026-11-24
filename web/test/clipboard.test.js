import assert from "node:assert/strict";
import test from "node:test";

import {
  buildOTPDirectLink,
  buildRecentMailDirectLink,
  getOTPOrigin,
  normalizeOTPOrigin,
  setOTPOrigin,
  copyText,
} from "../src/utils/clipboard.js";
import { buildAliasExportLine } from "../src/utils/aliasExport.js";
import { buildProjectMailboxExportText } from "../src/utils/projectMailboxExport.js";
import { buildInboxShareLink } from "../src/utils/inbox.js";

test("custom OTP domain applies to links and exports and can be cleared", (t) => {
  const data = new Map();
  const descriptor = Object.getOwnPropertyDescriptor(globalThis, "localStorage");
  Object.defineProperty(globalThis, "localStorage", { configurable: true, value: {
    getItem: (key) => data.get(key) ?? null,
    setItem: (key, value) => data.set(key, value),
    removeItem: (key) => data.delete(key),
  } });
  t.after(() => {
    if (descriptor) Object.defineProperty(globalThis, "localStorage", descriptor);
    else delete globalThis.localStorage;
  });
  const origin = "https://admin.example.test";
  const path = "/api/v1/otp?token=derived_a%2Bb%2Fc";
  assert.equal(setOTPOrigin(" https://OTP.example.test/ "), "https://otp.example.test");
  assert.equal(buildOTPDirectLink(path, origin), `https://otp.example.test${path}`);
  assert.equal(buildInboxShareLink("/inbox/view?token=icm_test", origin), "https://otp.example.test/inbox/view?token=icm_test");
  assert.equal(buildAliasExportLine({ address: "a@example.test", otpUrlPath: path }, "otp", origin), `a@example.test-----https://otp.example.test${path}`);
  assert.equal(buildProjectMailboxExportText([{ address: "a@example.test", otp_url_path: path }], origin), `a@example.test-----https://otp.example.test${path}`);
  assert.equal(buildRecentMailDirectLink("/api/v1/mail/recent?api_key=legacy", origin), `${origin}/api/v1/mail/recent?api_key=legacy`);
  assert.throws(() => setOTPOrigin("https://bad.example/path"));
  assert.equal(getOTPOrigin(origin), "https://otp.example.test");
  assert.equal(setOTPOrigin(" "), "");
  assert.equal(buildOTPDirectLink(path, origin), `${origin}${path}`);
  assert.equal(buildInboxShareLink("/inbox/view?token=icm_test", origin), `${origin}/inbox/view?token=icm_test`);
  Object.defineProperty(globalThis, "localStorage", { configurable: true, get() { throw new Error("denied"); } });
  assert.equal(buildOTPDirectLink(path, origin), `${origin}${path}`);
  assert.throws(() => setOTPOrigin("https://otp.example.test"), /denied/);
});

test("OTP domains reject credentials, insecure schemes, paths and parameters", () => {
  for (const value of ["http://otp.example.test", "javascript:alert(1)", "https://user:password@otp.example.test", "https://otp.example.test/api/v1/otp", "https://otp.example.test?token=secret", "https://otp.example.test#fragment", "otp.example.test"]) {
    assert.throws(() => normalizeOTPOrigin(value));
  }
});

function fallbackDocument(copyResult = true) {
  const state = { appended: null, commands: [] };
  const document = {
    body: {
      appendChild(element) {
        state.appended = element;
        element.parentNode = this;
      },
      removeChild(element) {
        if (state.appended === element) state.appended = null;
      },
    },
    createElement(tagName) {
      assert.equal(tagName, "textarea");
      return {
        style: {},
        setAttribute() {},
        select() {},
        remove() {
          state.appended = null;
        },
      };
    },
    execCommand(command) {
      state.commands.push(command);
      return copyResult;
    },
  };
  return { document, state };
}

test("OTP direct links stay on the current origin", () => {
  assert.equal(
    buildOTPDirectLink(
      "/api/v1/otp?token=derived_a%2Bb%2Fc",
      "https://mail.example.test:8443",
    ),
    "https://mail.example.test:8443/api/v1/otp?token=derived_a%2Bb%2Fc",
  );
});

test("OTP direct links reject unsafe or unexpected paths", () => {
  for (const path of [
    "//attacker.example/api/v1/otp?token=secret",
    "https://attacker.example/api/v1/otp?token=secret",
    "/api/v1/mail/latest?token=secret",
    "/api/v1/otp?token=",
    "/api/v1/otp?token=one&token=two",
    "/api/v1/otp?token=secret&next=elsewhere",
    "/api/v1/otp?token=secret#fragment",
  ]) {
    assert.throws(
      () => buildOTPDirectLink(path, "https://mail.example.test"),
      /链接格式无效/,
    );
  }
});

test("copyText uses Clipboard API immediately in a secure context", async () => {
  const writes = [];
  const copied = await copyText("https://mail.example.test/direct", {
    clipboard: {
      writeText(value) {
        writes.push(value);
        return Promise.resolve();
      },
    },
    document: null,
    isSecureContext: true,
  });

  assert.equal(copied, true);
  assert.deepEqual(writes, ["https://mail.example.test/direct"]);
});

test("copyText falls back when Clipboard API rejects access", async () => {
  const { document, state } = fallbackDocument();
  const copied = await copyText("fallback value", {
    clipboard: {
      writeText() {
        return Promise.reject(new Error("denied"));
      },
    },
    document,
    isSecureContext: true,
  });

  assert.equal(copied, true);
  assert.deepEqual(state.commands, ["copy"]);
  assert.equal(state.appended, null);
});

test("copyText uses and cleans up the fallback outside secure contexts", async () => {
  const { document, state } = fallbackDocument(false);
  const copied = await copyText("fallback value", {
    clipboard: {
      writeText() {
        assert.fail("Clipboard API must not be used outside a secure context");
      },
    },
    document,
    isSecureContext: false,
  });

  assert.equal(copied, false);
  assert.deepEqual(state.commands, ["copy"]);
  assert.equal(state.appended, null);
});
