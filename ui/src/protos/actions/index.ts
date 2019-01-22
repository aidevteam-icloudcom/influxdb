// Libraries
import {Dispatch} from 'redux'

// APIs
import {
  getProtos as getProtosAJAX,
  createDashFromProto as createDashFromProtoAJAX,
} from 'src/protos/apis/'

// Utils
import {addDashboardIDToCells} from 'src/dashboards/apis/v2/'
import {addLabelDefaults} from 'src/shared/utils/labels'

// Actions
import {loadDashboard} from 'src/dashboards/actions/v2/'

// Types
import {Proto} from 'src/api'
import {Dashboard} from 'src/types/v2'

export enum ActionTypes {
  LoadProto = 'LOAD_PROTO',
}

export type Action = LoadProtoAction

interface LoadProtoAction {
  type: ActionTypes.LoadProto
  payload: {
    proto: Proto
  }
}

export const loadProto = (proto: Proto): LoadProtoAction => ({
  type: ActionTypes.LoadProto,
  payload: {proto},
})

export const getProtos = () => async (dispatch: Dispatch<Action>) => {
  try {
    const {protos} = await getProtosAJAX()
    protos.forEach(p => {
      dispatch(loadProto(p))
    })
  } catch (error) {
    console.error(error)
  }
}

export const createDashFromProto = (
  protoID: string,
  orgID: string
) => async dispatch => {
  try {
    const {dashboards} = await createDashFromProtoAJAX(protoID, orgID)

    dashboards.forEach((d: Dashboard) => {
      const updatedDashboard = {
        ...d,
        labels: d.labels.map(addLabelDefaults),
        cells: addDashboardIDToCells(d.cells, d.id),
      }
      dispatch(loadDashboard(updatedDashboard))
    })
  } catch (error) {
    console.error(error)
  }
}
