"""Project recorded issue intervals onto a timeline without summing concurrent work."""

import datetime
import html

from dev_check import write_json


def micros(value):
    return round(datetime.datetime.fromisoformat(value.replace('Z', '+00:00')).timestamp() * 1_000_000)


def union(intervals):
    total, edge = 0, None
    for start, end in sorted(intervals):
        if edge is None or start >= edge:
            total += end - start
        elif end > edge:
            total += end - edge
        edge = max(end, edge) if edge is not None else end
    return total


def uncovered(intervals, start, stop):
    gaps, edge = [], start
    for left, right in sorted(intervals):
        if left > edge:
            gaps.append({'start_seconds': (edge-start)/1e6, 'end_seconds': (left-start)/1e6})
        edge = max(edge, right)
    if edge < stop:
        gaps.append({'start_seconds': (edge-start)/1e6, 'end_seconds': (stop-start)/1e6})
    return gaps


def export_report(journal, spans, directory, observed_at):
    start, stop = micros(journal['started_at']), micros(journal.get('finished_at') or observed_at)
    if stop < start or stop > micros(observed_at) or any(micros(e['at']) > stop for e in journal['events']):
        raise ValueError('inconsistent clock or observation precedes evidence')
    completed = [s for s in spans.values() if 'end' in s]
    coverage = [(micros(s['at']), micros(s['end'])) for s in completed]
    by_stage = {stage: union([(micros(s['at']), micros(s['end'])) for s in completed if s['stage'] == stage]) / 1e6
                for stage in sorted({s['stage'] for s in spans.values()})}
    summary = dict(schema='orbit.flow-summary.v1', repository=journal['repository'], issue=journal['issue'],
                   started_at=journal['started_at'], finished_at=journal.get('finished_at'), observed_at=observed_at,
                   window_seconds=(stop-start)/1e6, completed_interval_union_seconds=union(coverage)/1e6,
                   without_completed_interval_evidence_seconds=(stop-start-union(coverage))/1e6,
                   uncovered_intervals=uncovered(coverage, start, stop),
                   stage_union_seconds=by_stage, unfinished_spans=[s['id'] for s in spans.values() if 'end' not in s],
                   notes=[e for e in journal['events'] if e['type'] == 'note'],
                   failed_spans=[s['id'] for s in completed if s['result'] == 'failed'],
                   prior_requirements='unobserved', mutation_authority=False,
                   interpretation='Elapsed windows, not active coding or attention time. Stage unions overlap; do not sum them. Missing history is not reconstructed.')
    trace = [dict(ph='M', name='thread_name', pid=1, tid=0, args={'name': 'Observation notes'})]
    for event in summary['notes']:
        trace.append(dict(ph='i', s='t', name=event['label'], cat='observation', pid=1, tid=0,
                          ts=micros(event['at'])-start, args={'id': event['id'], 'provenance': 'observation note; no execution duration'}))
    rows = sorted(spans.values(), key=lambda s: (micros(s['at']), s['id']))
    svg = ['<svg class="orbit-flow" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 360 {}" role="img" aria-labelledby="flow-title flow-desc">'.format(150+len(rows)*64),
           '<title id="flow-title">Issue #{} recorded phase windows</title>'.format(journal['issue']),
           '<desc id="flow-desc">Concurrent rows overlap in elapsed time. Unfinished rows are observations, not success. Prior requirements are unobserved.</desc>',
           '<style>.orbit-flow text{font:13px system-ui;fill:#17314b;text-anchor:start}.orbit-flow .bg{fill:#fff}.orbit-flow .bar{fill:#0f766e}.orbit-flow .bad{fill:#c2410c}.orbit-flow .open{fill:#64748b}@media(prefers-color-scheme:dark){.orbit-flow text{fill:#e8f1fc}.orbit-flow .bg{fill:#132338}.orbit-flow .bar{fill:#5eead4}.orbit-flow .bad{fill:#fb923c}}</style>',
           '<rect class="bg" width="360" height="100%"/>',
           '<text x="14" y="24">Issue #{} · {:.1f} min observed window</text>'.format(journal['issue'], (stop-start)/6e7),
           '<text x="14" y="46">Phase windows overlap; not active work time.</text>',
           '<text x="14" y="68">Earlier requirements: unobserved.</text>',
           '<text x="14" y="88">Notes: {} · Unfinished windows: {}</text>'.format(len(summary['notes']), len(summary['unfinished_spans']))]
    for index, span in enumerate(rows, 1):
        a, b = micros(span['at']), micros(span.get('end', observed_at))
        details = {k: v for k, v in span.items() if k not in ('type', 'at', 'end')}
        trace.extend([dict(ph='M', name='thread_name', pid=1, tid=index, args={'name': span['stage'] + ' / ' + span['actor']}),
                      dict(ph='X', name=span['label'], cat=span['stage'], pid=1, tid=index, ts=a-start, dur=b-a,
                           args=dict(details, incomplete='end' not in span))])
        y = 112 + (index-1)*64
        text = '{} / {} · {} · {:.1f}s'.format(span['stage'], span['actor'][:10], span.get('result', 'unfinished'), (b-a)/1e6)
        svg.append('<g><title>{}</title><text x="14" y="{}">{}</text><text x="14" y="{}">{}</text>'.format(
            html.escape(span['label']), y, html.escape(span['label'][:24]), y+18, html.escape(text)))
        x, width = 14+332*(a-start)/max(1, stop-start), max(1, 332*(b-a)/max(1, stop-start))
        color = 'open' if 'end' not in span else 'bad' if span['result'] != 'passed' else 'bar'
        svg.append('<rect class="{}" x="{:.2f}" y="{}" width="{:.2f}" height="12" rx="3"/></g>'.format(color, x, y+27, width))
    svg.append('</svg>')
    write_json(directory/'trace.json', {'traceEvents': trace, 'displayTimeUnit': 'ms'})
    write_json(directory/'summary.json', summary)
    (directory/'timeline.svg').write_text('\n'.join(svg))
    return summary
