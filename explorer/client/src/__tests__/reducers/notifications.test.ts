import * as jsonapi from '@chainlink/json-api-client'
import reducer, { INITIAL_STATE } from '../../reducers'
import { partialAsFull } from '../support/mocks'

describe('reducers/jobRuns', () => {
  describe('FETCH_ADMIN_SIGNIN_ERROR', () => {
    it('adds a notification for AuthenticationError', () => {
      const response = partialAsFull<Response>({})
      const action = {
        type: 'FETCH_ADMIN_SIGNIN_ERROR',
        error: new jsonapi.AuthenticationError(response),
      }
      const state = reducer(INITIAL_STATE, action)

      expect(state.notifications.errors).toEqual([
        'Invalid username and password.',
      ])
    })
  })
})
