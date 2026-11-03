// Existing UI fixtures assert Chinese copy. Locale behavior has separate tests.
if (typeof navigator !== 'undefined') {
  Object.defineProperty(navigator, 'language', {configurable: true, value: 'zh-CN'});
  Object.defineProperty(navigator, 'languages', {configurable: true, value: ['zh-CN']});
}
