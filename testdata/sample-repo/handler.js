// Simple request handlers with a few planted defects.

function runUserCode(input) {
  // BUG (insecure_default / code injection, Critical): eval on user input.
  return eval(input);
}

function isEqual(a, b) {
  // BUG (logic, Medium): loose equality causes type-coercion bugs.
  return a == b;
}

function lastItem(arr) {
  // BUG (off_by_one, High): off-by-one indexing returns undefined.
  return arr[arr.length];
}

function getConfig(cfg) {
  // BUG (null_dereference, High): cfg.options may be undefined.
  return cfg.options.timeout;
}

module.exports = { runUserCode, isEqual, lastItem, getConfig };
