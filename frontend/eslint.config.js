import js from '@eslint/js'
import pluginVue from 'eslint-plugin-vue'
import { defineConfigWithVueTs, vueTsConfigs } from '@vue/eslint-config-typescript'

export default defineConfigWithVueTs(
  {
    ignores: [
      'dist/**',
      'coverage/**',
      'src/gen/**',
      'playwright-report/**',
      'test-results/**',
      'node_modules/**',
      // Its own inferred type cannot be named under pnpm's nested store, and
      // type-checking the lint config buys nothing.
      'eslint.config.js',
    ],
  },
  js.configs.recommended,
  pluginVue.configs['flat/recommended'],
  vueTsConfigs.recommendedTypeChecked,
  {
    languageOptions: {
      parserOptions: {
        projectService: true,
        tsconfigRootDir: import.meta.dirname,
      },
    },
    rules: {
      // any is how a typed contract quietly stops being enforced. The generated
      // client already describes every response, so reaching for any means
      // something is being worked around rather than modelled.
      '@typescript-eslint/no-explicit-any': 'error',
      '@typescript-eslint/no-unsafe-assignment': 'error',
      '@typescript-eslint/no-unsafe-member-access': 'error',
      '@typescript-eslint/no-unsafe-call': 'error',
      '@typescript-eslint/no-unsafe-return': 'error',
      '@typescript-eslint/no-floating-promises': 'error',
      '@typescript-eslint/no-misused-promises': 'error',
      '@typescript-eslint/consistent-type-imports': 'error',
      '@typescript-eslint/no-non-null-assertion': 'error',
      'no-console': ['error', { allow: ['warn', 'error'] }],
      eqeqeq: ['error', 'always'],
      'vue/multi-word-component-names': 'error',
      /*
       * A component used in a template and not registered renders as nothing.
       *
       * Vue treats an unknown tag as a custom element and says so only as a
       * runtime warning in the console, so `<VButton>Review</VButton>` becomes
       * an empty element that vue-tsc, the unit tests and Playwright all pass
       * over. The release queue's Review buttons and its state filter were
       * both missing this way, and every gate passed. This rule is the only
       * one that catches it before a person notices.
       */
      'vue/no-undef-components': 'error',
      'vue/component-api-style': ['error', ['script-setup']],
      'vue/define-props-declaration': ['error', 'type-based'],
      // Prettier owns whitespace. Leaving these on makes the two tools disagree
      // and turns formatting into a lint failure nobody can act on.
      'vue/max-attributes-per-line': 'off',
      'vue/singleline-html-element-content-newline': 'off',
      'vue/html-self-closing': 'off',
      'vue/html-indent': 'off',
      'vue/html-closing-bracket-newline': 'off',
      'vue/first-attribute-linebreak': 'off',
    },
  },
)
