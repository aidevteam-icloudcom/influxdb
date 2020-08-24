// Libraries
import {Dispatch} from 'react'
// import {fromFlux as parse} from '@influxdata/giraffe'

// API
// import {runQuery} from 'src/shared/apis/query'

// Types
import {RemoteDataState, GetState} from 'src/types'
// Utils
import {getOrg} from 'src/organizations/selectors'

// Actions
import {
  setSchema,
  Action as BucketAction,
} from 'src/shared/actions/schemaCreator'
import {notify, Action as NotifyAction} from 'src/shared/actions/notifications'

// Constants
import {getBucketsFailed} from 'src/shared/copy/notifications'

// DUMMY DATA TO DELETE
import {results} from 'src/notebooks/pipes/Data/dummyData'

type Action = BucketAction | NotifyAction

export const fetchSchemaForBucket = async (
  bucketName: string, // tslint:disable-line
  orgID: string // tslint:disable-line
) => {
  // TODO(ariel): make this work with the query & the time range
  // const text = `import "influxdata/influxdb/v1"
  // from(bucket: "${bucketName}")
  // |> range(start: -1h)
  // |> first()
  // |> v1.fieldsAsCols()`

  // const res = await runQuery(orgID, text)
  //   .promise.then(raw => {
  //     if (raw.type !== 'SUCCESS') {
  //       throw new Error(raw.message)
  //     }

  //     return raw
  //   })
  //   .then(raw => {
  //     return {
  //       source: text,
  //       raw: raw.csv,
  //       parsed: parse(raw.csv),
  //       error: null,
  //     }
  //   })

  const result = await new Promise(resolve => resolve(results))
  return result
}

export const getAndSetBucketSchema = (bucketName: string) => async (
  dispatch: Dispatch<Action>,
  getState: GetState
) => {
  try {
    const state = getState()
    if (bucketName) {
      dispatch(setSchema(RemoteDataState.Loading, bucketName, []))
    }
    const orgID = getOrg(state).id
    const schema = await fetchSchemaForBucket(bucketName, orgID)
    dispatch(setSchema(RemoteDataState.Done, bucketName, schema as any[]))
  } catch (error) {
    console.error(error)
    dispatch(setSchema(RemoteDataState.Error))
    dispatch(notify(getBucketsFailed()))
  }
}
