#!/usr/bin/env python3
"""Small registry tests: manifests, confinement, decoded sprites and shared roles."""
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from PIL import Image
import studio_cast as cast


class CastTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory()
        self.root=Path(self.temp.name)
        self.directory=self.root/'test-pair';self.directory.mkdir()
        self.manifest={'id':'test-pair','name':'A Test Pair','speakers':[
            {'id':'first','name':'First','role':'Curious questions.','idle':'first.png'},
            {'id':'second','name':'Second','role':'Careful explanations.','idle':'second.png'}]}
        for name in ('first','second'):
            Image.new('RGBA',(32,40),(80,160,140,255)).save(self.directory/f'{name}.png')
        self.path=self.directory/'cast.json'
        self.save()
        self.env=patch.dict(os.environ,{'STUDIO_CAST_DIR':str(self.root)});self.env.start()

    def tearDown(self):
        self.env.stop();self.temp.cleanup()

    def save(self):
        self.path.write_text(json.dumps(self.manifest))

    def test_manifest_and_registry_share_names_roles_and_still_modes(self):
        validated=cast.validate_manifest(self.path)
        self.assertEqual(validated['speakers'][0]['idle'],'first.png')
        pair=cast.get_pair('test-pair')
        self.assertTrue(pair['available'])
        self.assertEqual(pair['speakers'][1]['role'],'Careful explanations.')
        self.assertTrue(Path(pair['speakers'][0]['idle']).is_absolute())
        self.assertEqual(set(pair['presenter_animation'].values()),{'audio-reactive-still'})
        public=cast.public_pair(pair)
        self.assertEqual(public['speakers'],['First','Second'])
        self.assertNotIn(str(self.root),json.dumps(public))
        self.assertIn('cog-axiom',[pair['id'] for pair in cast.list_pairs()])

    def test_rejects_traversal_urls_symlinks_and_reserved_ids(self):
        for filename in ('../first.png','/tmp/first.png','https://example.com/a.png'):
            with self.subTest(filename=filename):
                self.manifest['speakers'][0]['idle']=filename;self.save()
                with self.assertRaises(ValueError):cast.validate_manifest(self.path)
        self.manifest['speakers'][0]['idle']='link.png'
        (self.directory/'link.png').symlink_to(self.directory/'first.png');self.save()
        with self.assertRaises(ValueError):cast.validate_manifest(self.path)
        self.manifest['speakers'][0]['idle']='first.png'
        for id_ in ('cog-axiom','trailing-','double--hyphen'):
            self.manifest['id']=id_;self.save()
            with self.assertRaises(ValueError):cast.validate_manifest(self.path)

    def test_rejects_invalid_truncated_or_overlarge_images(self):
        first=self.directory/'first.png'
        first.write_bytes(b'not a PNG')
        with self.assertRaises((ValueError,OSError)):cast.validate_manifest(self.path)
        Image.new('RGBA',(4097,32)).save(first)
        with self.assertRaises(ValueError):cast.validate_manifest(self.path)
        Image.new('RGBA',(32,40)).save(first)
        raw=first.read_bytes();first.write_bytes(raw[:40])
        with self.assertRaises((ValueError,OSError)):cast.validate_manifest(self.path)

    def test_invalid_manifest_is_not_exposed_to_planner(self):
        self.manifest['speakers'][0]['idle']='missing.png';self.save()
        self.assertNotIn('test-pair',[pair['id'] for pair in cast.list_pairs()])


if __name__=='__main__':unittest.main()
