import { Actions } from './actions'
import { Reducer } from 'redux'

interface State {
  id?: {
    attributes: {
      id: string
      name: string
      url?: string
      createdAt: string
    }
  }
}

const INITIAL_STATE: State = {}

export const adminOperatorsShow: Reducer<State, Actions> = (
  state = INITIAL_STATE,
  action,
) => {
  switch (action.type) {
    case 'FETCH_ADMIN_OPERATOR_SUCCEEDED': {
      return action.data.chainlinkNodes
    }
    default: {
      return state
    }
  }
}

export default adminOperatorsShow
