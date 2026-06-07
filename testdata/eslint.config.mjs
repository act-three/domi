// ESLint config for client.js, exercised by the Go test
// TestClientJSNoShadowedVars. It enables just the two rules that catch a
// helper parameter masking an outer binding — like restoreSnapshot(ver)
// hiding run()'s ver, which turns the intended update into a no-op
// self-assignment. tsc covers undefined names separately, so no-undef
// stays off and the globals list can too.
export default [
  {
    languageOptions: { ecmaVersion: 2022, sourceType: 'module' },
    rules: {
      'no-shadow': 'error',
      'no-self-assign': 'error',
    },
  },
];
