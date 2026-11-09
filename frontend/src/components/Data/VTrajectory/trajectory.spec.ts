import { describe, expect, it } from 'vitest'

import {
  describeTrajectory,
  releaseTrajectory,
  writeTrajectory,
  type StepState,
} from './trajectory'

const states = (steps: { state: StepState }[]) => steps.map((step) => step.state)

describe('a write request’s trajectory', () => {
  it('waits at approval while pending', () => {
    const { steps, caption } = writeTrajectory('pending')
    expect(states(steps)).toEqual(['done', 'done', 'current', 'todo'])
    expect(caption).toBe('Awaiting approval')
  })

  it('stops at approval when rejected or withdrawn', () => {
    expect(states(writeTrajectory('rejected').steps)).toEqual(['done', 'done', 'stopped', 'todo'])
    expect(states(writeTrajectory('withdrawn').steps)).toEqual(['done', 'done', 'stopped', 'todo'])
    expect(writeTrajectory('rejected').tone).toBe('halt')
    expect(writeTrajectory('withdrawn').tone).toBe('muted')
  })

  it('stops at applied when the run failed, and completes when it ran', () => {
    expect(states(writeTrajectory('failed').steps)).toEqual(['done', 'done', 'done', 'stopped'])
    expect(states(writeTrajectory('applied').steps)).toEqual(['done', 'done', 'done', 'done'])
  })

  it('never stops at the guard, because a refused write is never a request', () => {
    for (const state of ['pending', 'rejected', 'withdrawn', 'applied', 'failed'] as const) {
      expect(writeTrajectory(state).steps[1]?.state).toBe('done')
    }
  })
})

describe('a release’s trajectory', () => {
  it('follows the same four steps', () => {
    expect(states(releaseTrajectory('pending').steps)).toEqual(['done', 'done', 'current', 'todo'])
    expect(states(releaseTrajectory('approved').steps)).toEqual(['done', 'done', 'done', 'current'])
    expect(states(releaseTrajectory('rejected').steps)).toEqual(['done', 'done', 'stopped', 'todo'])
    expect(states(releaseTrajectory('failed').steps)).toEqual(['done', 'done', 'done', 'stopped'])
    expect(states(releaseTrajectory('applied').steps)).toEqual(['done', 'done', 'done', 'done'])
  })
})

it('reads out in words', () => {
  expect(describeTrajectory(writeTrajectory('pending'))).toBe(
    'Awaiting approval: Submitted done, Guard done, Approval waiting, Applied to do',
  )
})
