import { describe, expect, it } from 'vitest';
import { decodeTagBatch, tagBatchPatterns } from '../src/store/tag-batches';

describe('tag value batches', () => {
    it('combines siblings and removes overlapping selected subscriptions', () => {
        expect(tagBatchPatterns(['acme.BUSES.*.meta.lat', 'acme.BUSES.*.meta.lon', 'acme.BUSES.One.meta.lat', 'acme.Routes.One.route.coordinates.>'])).toEqual([
            'xact.internal.bcast.tagbatch.acme.BUSES.*.meta.>',
            'xact.internal.bcast.tagbatch.acme.Routes.One.route.coordinates.>',
        ]);
        expect(tagBatchPatterns(['acme.BUSES.>', 'acme.BUSES.One.meta.lat'])).toEqual(['xact.internal.bcast.tagbatch.acme.BUSES.>']);
    });

    it('applies only changed entries while retaining the replay fields', () => {
        const values = { 'acme.Bus.meta.lat': { type: 'value', value: 49.283456 }, 'acme.Bus.meta.online': { type: 'value', value: true } };
        expect(decodeTagBatch('xact.internal.bcast.tagbatch.acme.Bus.meta.all', { values, changed: ['acme.Bus.meta.lat'] }, 'acme')).toEqual([
            ['acme.Bus.meta.lat', values['acme.Bus.meta.lat']],
        ]);
        expect(decodeTagBatch('xact.internal.bcast.tagbatch.acme.Bus.meta.all', { values, changed: [] }, 'acme')).toEqual([]);
    });

    it('rejects malformed and foreign tenant/group updates without partial application', () => {
        const subject = 'xact.internal.bcast.tagbatch.acme.Bus.meta.all';
        for (const path of ['other.Bus.meta.lat', 'acme.Bus.status.lat', 'acme.Bus.meta.nested.lat', 'absent']) {
            expect(decodeTagBatch(subject, { values: { [path]: { type: 'value', value: 1 } }, changed: [path] }, 'acme')).toEqual([]);
        }
        expect(decodeTagBatch(subject, { values: {}, changed: ['acme.Bus.meta.lat'] }, 'acme')).toEqual([]);
        expect(decodeTagBatch(subject, { values: {}, changed: 'invalid' }, 'acme')).toEqual([]);
        expect(decodeTagBatch(subject, { values: {}, changed: [] }, 'other')).toEqual([]);
    });
});
