const test = require('node:test')
const assert = require('node:assert')
const { problem } = require('./check-url')

test('a release build needs an https address', () => {
  assert.match(problem('release', ''), /not set/)
  assert.match(problem('release', undefined), /not set/)
  assert.match(problem('release', 'http://order.example.com'), /https/)
  assert.match(problem('release', 'order.example.com'), /not an address|https/)
  assert.match(problem('release', 'https://user:pw@order.example.com'), /plain/)
  assert.equal(problem('release', 'https://order.example.com'), '')
  assert.equal(problem('release', 'https://order.example.com/'), '')
})

test('a debug build may point anywhere', () => {
  assert.equal(problem('debug', ''), '')
  assert.equal(problem('debug', 'http://10.0.2.2:8080'), '')
})
