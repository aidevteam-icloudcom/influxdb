import {PRESENTATION_MODE_ANIMATION_DELAY} from '../constants'

import {notify} from 'src/shared/actions/notifications'
import {presentationMode} from 'src/shared/copy/notifications'

import {Dispatch} from 'redux'

import {TimeZone, Theme} from 'src/types'

export enum ActionTypes {
  EnablePresentationMode = 'ENABLE_PRESENTATION_MODE',
  DisablePresentationMode = 'DISABLE_PRESENTATION_MODE',
  SetAutoRefresh = 'SET_AUTOREFRESH',
  SetTimeZone = 'SET_APP_TIME_ZONE',
  TemplateControlBarVisibilityToggled = 'TemplateControlBarVisibilityToggledAction',
  Noop = 'NOOP',
}

export type Action =
  | ReturnType<typeof enablePresentationMode>
  | ReturnType<typeof disablePresentationMode>
  | ReturnType<typeof setAutoRefresh>
  | ReturnType<typeof setTimeZone>
  | ReturnType<typeof setTheme>

export const setTheme = (theme: Theme) => ({type: 'SET_THEME', theme} as const)

// ephemeral state action creators

export const enablePresentationMode = () =>
  ({
    type: ActionTypes.EnablePresentationMode,
  } as const)

export const disablePresentationMode = () =>
  ({
    type: ActionTypes.DisablePresentationMode,
  } as const)

export const delayEnablePresentationMode = () => (
  dispatch: Dispatch<ReturnType<typeof enablePresentationMode>>
): NodeJS.Timer =>
  setTimeout(() => {
    dispatch(enablePresentationMode())
    notify(presentationMode())
  }, PRESENTATION_MODE_ANIMATION_DELAY)

// persistent state action creators

export const setAutoRefresh = (milliseconds: number) =>
  ({
    type: ActionTypes.SetAutoRefresh,
    payload: {
      milliseconds,
    },
  } as const)

export const setTimeZone = (timeZone: TimeZone) =>
  ({
    type: ActionTypes.SetTimeZone,
    payload: {timeZone},
  } as const)
