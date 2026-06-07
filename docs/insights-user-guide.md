# Insights User Guide

Use the Insights section in the sidebar to understand project activity after tasks, schedules, agents, and channels have started producing work.

## Sidebar Pages

| UI Label | What It Is For |
|---|---|
| Grades | Proactive insights, health checks, knowledge signals, and idea grading. |
| Pulse | Upcoming work and generated pulse summaries. |
| Reflection | Historical task activity and generated reflections. |
| Analytics | Token usage, cost, model breakdowns, execution rates, duration trends, agent usage, frequent tasks, and failure trends. |

## Analytics

Analytics is the quantitative view of how OpenVibely is being used. Open it from the Insights section of the sidebar.

### Token Usage And Cost

| Chart / Table | What It Shows |
|---|---|
| Token Usage chart | Input, output, cached, and reasoning tokens over time, filterable by model. |
| Token Usage Breakdown table | Per-provider, per-model columns for input tokens, output tokens, cache tokens, reasoning tokens, total tokens, and estimated cost. |
| Model Breakdown by Tokens | Pie chart showing relative token share across configured models. |

### Provider Accounts

OAuth-connected provider accounts (Anthropic, OpenAI) show a usage snapshot card so you can see which account is consuming capacity.

### Performance

| Chart | What It Shows |
|---|---|
| Average Execution Time by Model | Compare latency across providers and models. |
| Execution rate | Task execution frequency over time. |
| Duration trend | Whether execution time is increasing or decreasing. |
| Agent usage | Which agents are handling most work. |
| Frequent task | Which task types recur most often. |
| Failure trend | Whether task failure rate is increasing or stable. |

Analytics charts render in the browser timezone so time-axis labels match local working hours.

## How Insights Fit The Workflow

Use the task board for live execution status. Use Insights when you want to step back and answer questions like:

- Are tasks succeeding or failing more often?
- Which agents or models are most active?
- What work is coming up this week?
- What historical trends are emerging?
- Which model is consuming the most tokens or cost?

Use Analytics specifically to understand provider spend, token consumption by model, and execution performance across the project.

## Related Pages

For the full reference see <a href="https://docs.openvibely.ai/insights" target="_blank" rel="noopener noreferrer">Insights</a> on the docs site.

| Area | See Also |
|---|---|
| Tasks | Task activity drives most insight data. |
| Alerts | Failures and follow-up events may also appear in trends. |
| Models | Model choice directly affects token usage and cost figures in Analytics. |
| Workers | Execution rate and duration trends reflect worker capacity and queue pressure. |
