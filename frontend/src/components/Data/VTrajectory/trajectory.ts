export type StepState = 'done' | 'current' | 'stopped' | 'todo'

export type Step = { label: string; state: StepState }

export type Trajectory = {
  steps: Step[]
  caption: string
  tone: 'target' | 'halt' | 'go' | 'muted'
}

export type WriteState = 'pending' | 'rejected' | 'withdrawn' | 'applied' | 'failed'
export type ReleaseState = 'pending' | 'approved' | 'rejected' | 'applied' | 'failed'

export const stepLabels = ['Submitted', 'Guard', 'Approval', 'Applied'] as const

/** Four states, one per step: everything before `at` done, `at` in `state`, the rest to do. */
function reach(at: number, state: StepState): Step[] {
  return stepLabels.map((label, index) => ({
    label,
    state: index < at ? 'done' : index === at ? state : 'todo',
  }))
}

const allDone = (): Step[] => stepLabels.map((label) => ({ label, state: 'done' }))

/**
 * A write request's path: submitted, guard, approval, applied. A request only
 * exists once the guard let it through, so it can never stop at the guard.
 */
export function writeTrajectory(state: WriteState): Trajectory {
  switch (state) {
    case 'pending':
      return { steps: reach(2, 'current'), caption: 'Awaiting approval', tone: 'target' }
    case 'rejected':
      return { steps: reach(2, 'stopped'), caption: 'Rejected', tone: 'halt' }
    case 'withdrawn':
      return { steps: reach(2, 'stopped'), caption: 'Withdrawn', tone: 'muted' }
    case 'failed':
      return { steps: reach(3, 'stopped'), caption: 'Failed', tone: 'halt' }
    case 'applied':
      return { steps: allDone(), caption: 'Applied', tone: 'go' }
  }
}

export function releaseTrajectory(state: ReleaseState): Trajectory {
  switch (state) {
    case 'pending':
      return { steps: reach(2, 'current'), caption: 'Awaiting approval', tone: 'target' }
    case 'approved':
      return { steps: reach(3, 'current'), caption: 'Approved · awaiting apply', tone: 'target' }
    case 'rejected':
      return { steps: reach(2, 'stopped'), caption: 'Rejected', tone: 'halt' }
    case 'failed':
      return { steps: reach(3, 'stopped'), caption: 'Failed', tone: 'halt' }
    case 'applied':
      return { steps: allDone(), caption: 'Applied', tone: 'go' }
  }
}

/** The trajectory in words, for whoever cannot see the stars. */
export function describeTrajectory(trajectory: Trajectory): string {
  const words: Record<StepState, string> = {
    done: 'done',
    current: 'waiting',
    stopped: 'stopped',
    todo: 'to do',
  }
  const path = trajectory.steps.map((step) => `${step.label} ${words[step.state]}`).join(', ')
  return `${trajectory.caption}: ${path}`
}
