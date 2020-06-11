// Libraries
import React, {FC} from 'react'
import {BothResults} from 'src/notebooks/context/query'
import {AutoSizer} from 'react-virtualized'

// Components
import RawFluxDataTable from 'src/timeMachine/components/RawFluxDataTable'
import Resizer from 'src/notebooks/pipes/Query/Resizer'

// Types
import {PipeData} from 'src/notebooks/index'

interface Props {
  data: PipeData
  results: BothResults
  onUpdate: (data: any) => void
}

const Results: FC<Props> = ({results, onUpdate, data}) => {
  const resultsExist = !!results.raw

  return (
    <div className="notebook-raw-data">
      <Resizer data={data} onUpdate={onUpdate} resizingEnabled={resultsExist}>
        <AutoSizer>
          {({width, height}) =>
            width &&
            height && (
              <RawFluxDataTable
                files={[results.raw]}
                width={width}
                height={height}
              />
            )
          }
        </AutoSizer>
      </Resizer>
    </div>
  )
}

export default Results
