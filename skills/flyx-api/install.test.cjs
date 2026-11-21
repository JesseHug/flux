const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { install } = require("./install.cjs");

function fixture(t) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "flux-skill-"));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  return root;
}

test("fresh installation, repeated linking and uninstall", (t) => {
  const root = fixture(t);
  install("link", root);
  install("link", root);
  assert.equal(fs.realpathSync(path.join(root, "flux-api")), __dirname);
  install("unlink", root);
  assert.equal(fs.existsSync(path.join(root, "flux-api")), false);
});

test("migrate recognized old packages without loading the skill twice", (t) => {
  const root = fixture(t);
  const oldFlvx = path.join(root, "old-flvx-package");
  fs.mkdirSync(oldFlvx);
  fs.writeFileSync(path.join(oldFlvx, "package.json"), '{"name":"@flvx/skill-api"}');
  fs.symlinkSync(oldFlvx, path.join(root, "flvx-api"));

  const oldFlyx = path.join(root, "old-flyx-package");
  fs.mkdirSync(oldFlyx);
  fs.writeFileSync(path.join(oldFlyx, "package.json"), '{"name":"@flyx/skill-api"}');
  fs.symlinkSync(oldFlyx, path.join(root, "flyx-api"));

  install("link", root);
  assert.equal(fs.existsSync(path.join(root, "flvx-api")), false);
  assert.equal(fs.existsSync(path.join(root, "flyx-api")), false);
  assert.equal(fs.realpathSync(path.join(root, "flux-api")), __dirname);
});

test("preserve directories and unrelated links", (t) => {
  const root = fixture(t);
  const target = path.join(root, "flux-api");
  fs.mkdirSync(target);
  assert.throws(() => install("link", root), /Preserving/);
  install("unlink", root);
  assert.equal(fs.statSync(target).isDirectory(), true);
  fs.rmdirSync(target);
  fs.symlinkSync("some-other-skill", target);
  assert.throws(() => install("link", root), /Preserving/);
  assert.equal(fs.readlinkSync(target), "some-other-skill");
});
