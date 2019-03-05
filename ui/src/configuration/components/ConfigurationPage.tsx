// Libraries
import React, {Component} from 'react'
import {withRouter, WithRouterProps} from 'react-router'

// Components
import {Page} from 'src/pageLayout'
import GetResources, {
  ResourceTypes,
} from 'src/configuration/components/GetResources'
import TabbedPageSection from 'src/shared/components/tabbed_page/TabbedPageSection'
import TabbedPage from 'src/shared/components/tabbed_page/TabbedPage'
import Labels from 'src/configuration/components/Labels'
import Settings from 'src/me/components/account/Settings'
import Tokens from 'src/me/components/account/Tokens'
import Buckets from 'src/configuration/components/Buckets'

// Decorators
import {ErrorHandling} from 'src/shared/decorators/errors'

interface OwnProps {
  activeTabUrl: string
}

type Props = OwnProps & WithRouterProps

@ErrorHandling
class ConfigurationPage extends Component<Props> {
  public render() {
    const {
      params: {tab},
    } = this.props

    return (
      <Page titleTag="Configuration">
        <Page.Header fullWidth={false}>
          <Page.Header.Left>
            <Page.Title title="Configuration" />
          </Page.Header.Left>
          <Page.Header.Right />
        </Page.Header>
        <Page.Contents fullWidth={false} scrollable={true}>
          <div className="col-xs-12">
            <TabbedPage
              name={'Configuration'}
              parentUrl={`/configuration`}
              activeTabUrl={tab}
            >
              <TabbedPageSection
                id="labels_tab"
                url="labels_tab"
                title="Labels"
              >
                <GetResources resource={ResourceTypes.Labels}>
                  <Labels />
                </GetResources>
              </TabbedPageSection>
              <TabbedPageSection
                id="buckets_tab"
                url="buckets_tab"
                title="Buckets"
              >
                <GetResources resource={ResourceTypes.Buckets}>
                  <Buckets />
                </GetResources>
              </TabbedPageSection>
              <TabbedPageSection
                id="settings_tab"
                url="settings_tab"
                title="Profile"
              >
                <Settings />
              </TabbedPageSection>
              <TabbedPageSection
                id="tokens_tab"
                url="tokens_tab"
                title="Tokens"
              >
                <Tokens />
              </TabbedPageSection>
            </TabbedPage>
          </div>
        </Page.Contents>
      </Page>
    )
  }
}

export default withRouter<OwnProps>(ConfigurationPage)
