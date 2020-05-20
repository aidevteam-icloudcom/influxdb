import React, {FC} from 'react'

import {Page} from '@influxdata/clockface'
import {NotebookProvider} from 'src/notebooks/context/notebook'
import Header from 'src/notebooks/components/Header'
import PipeList from 'src/notebooks/components/PipeList'
import NotebookPanel from 'src/notebooks/components/panel/NotebookPanel'

// NOTE: uncommon, but using this to scope the project
// within the page and not bleed it's dependancies outside
// of the feature flag
import 'src/notebooks/style.scss'

const NotebookPage: FC = () => {
  return (
    <NotebookProvider>
      <Page titleTag="Notebook">
        <Header />
        <Page.Contents fullWidth={true} scrollable={true}>
          <PipeList />
        </Page.Contents>
      </Page>
    </NotebookProvider>
  )
}

export {NotebookPanel}
export default NotebookPage
