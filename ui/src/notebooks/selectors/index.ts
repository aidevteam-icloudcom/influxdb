// Types
import {AppState} from 'src/types'

export const getSchemaByBucket = (state: AppState, bucketName: string) =>
  state.notebook.schema[bucketName]
