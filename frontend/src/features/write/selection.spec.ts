import { describe, expect, it } from 'vitest'

import { isTyping, neighbour } from './selection'

const ids = ['a', 'b', 'c']

describe('neighbour', () => {
  it('moves one request down or up', () => {
    expect(neighbour(ids, 'a', 1)).toBe('b')
    expect(neighbour(ids, 'c', -1)).toBe('b')
  })

  it('stops at either end rather than wrapping', () => {
    expect(neighbour(ids, 'c', 1)).toBe('c')
    expect(neighbour(ids, 'a', -1)).toBe('a')
  })

  it('starts from the top or the bottom when nothing in the list is being read', () => {
    expect(neighbour(ids, '', 1)).toBe('a')
    expect(neighbour(ids, 'gone', -1)).toBe('c')
    expect(neighbour([], 'a', 1)).toBeNull()
  })
})

describe('isTyping', () => {
  it('tells a field from the page', () => {
    expect(isTyping(document.createElement('input'))).toBe(true)
    expect(isTyping(document.createElement('textarea'))).toBe(true)
    expect(isTyping(document.createElement('select'))).toBe(true)
    expect(isTyping(document.createElement('button'))).toBe(false)
    expect(isTyping(document.body)).toBe(false)
    expect(isTyping(null)).toBe(false)
  })

  it('counts an editable region as a field', () => {
    const editor = document.createElement('div')
    editor.setAttribute('contenteditable', 'true')
    const inside = document.createElement('span')
    editor.append(inside)
    document.body.append(editor)
    expect(isTyping(inside)).toBe(true)
    editor.remove()
  })
})
