#!/usr/bin/env python3
"""Rebuild the original Cog/Axiom and Nova/Atlas PNG cast without external artwork."""
import argparse
import math
from pathlib import Path
from PIL import Image, ImageDraw

ROOT = Path(__file__).resolve().parent
S = 2

def make(name, expression):
    im = Image.new('RGBA', (600*S, 740*S))
    d = ImageDraw.Draw(im)
    def ellipse(box, fill, outline=None, width=1):
        d.ellipse(tuple(int(x*S) for x in box), fill=fill, outline=outline, width=width*S)
    def line(points, fill, width=1):
        d.line([(int(x*S),int(y*S)) for x,y in points], fill=fill,width=width*S,joint='curve')
    def poly(points, fill, outline=None, width=1):
        pts=[(int(x*S),int(y*S)) for x,y in points]
        d.polygon(pts,fill=fill)
        if outline: d.line(pts+[pts[0]],fill=outline,width=width*S,joint='curve')
    def rect(box,radius,fill,outline=None,width=1):
        d.rounded_rectangle(tuple(int(x*S) for x in box),radius*S,fill,outline,width*S)
    ink='#192E3B'; nova=name=='nova'; skin='#E8AE91' if nova else '#AD6D4F'; light='#F3C3A5' if nova else '#C98B66'
    hair='#343249' if nova else '#173F42'; shirt='#BAACDA' if nova else '#639E96'; shadow='#9284BB' if nova else '#437970'
    # Silhouette, neck and shoulders.
    if nova:
        ellipse((145,92,455,459),hair)
        rect((147,215,453,479),88,hair)
    rect((117,451,483,788),97,shirt,ink,7)
    poly([(155,497),(104,535),(69,715),(170,738),(206,529)],shirt,ink,7)
    poly([(443,497),(496,535),(530,715),(430,738),(395,529)],shirt,ink,7)
    rect((253,382,347,495),30,skin,ink,6)
    ellipse((257,374,343,439),'#CD9078' if nova else '#95563F')
    # Under-shirt, jacket lapels, seams.
    poly([(253,470),(300,512),(347,470),(370,742),(229,742)],'#F9E7CB' if nova else '#E7A470',ink,5)
    poly([(231,452),(299,514),(262,561),(211,532),(222,506),(191,486)],shadow,ink,5)
    poly([(368,452),(300,514),(338,561),(389,532),(378,506),(409,486)],shadow,ink,5)
    line([(301,558),(301,740)],ink,4)
    line([(168,594),(163,702)],shadow,5);line([(432,594),(437,702)],shadow,5)
    rect((354,592,410,612),5,'#E8E3DB',ink,3)
    ellipse((369,597,379,607),'#F08D71');line([(386,602),(399,602)],ink,2)
    # Ears and face.
    ellipse((158,254,215,332),skin,ink,5);ellipse((385,254,442,332),skin,ink,5)
    ellipse((178,146,422,411),light,ink,7)
    ellipse((200,286,243,311),'#E9A184' if nova else '#BF7958')
    ellipse((359,286,402,311),'#E9A184' if nova else '#BF7958')
    if nova:
        # Sweeping graphic fringe, simple gold hoop earrings.
        poly([(182,279),(158,224),(166,163),(202,107),(278,82),(351,98),(403,127),(440,181),(436,251),(413,298),(395,225),(349,209),(308,173),(267,224),(217,245)],hair,ink,5)
        line([(203,187),(225,150),(269,135)],'#696079',9)
        ellipse((164,309,192,351),'#E9BE73',ink,4);ellipse((171,316,185,340),hair)
        ellipse((408,309,436,351),'#E9BE73',ink,4);ellipse((415,316,429,340),hair)
    else:
        poly([(177,253),(149,210),(164,167),(149,129),(194,130),(212,97),(240,108),(285,79),(304,99),(358,92),(391,124),(424,148),(445,213),(418,265),(405,200),(359,198),(331,170),(289,191),(244,177),(207,214),(195,262)],hair,ink,6)
        line([(206,158),(238,138),(265,143)],'#3C6662',10)
        line([(277,128),(306,116),(337,124)],'#3C6662',8)
    # Brows, expressive eyes, nose and mouth.
    line([(222,263),(240,255),(260,259)],ink,6);line([(338,259),(358,255),(379,263)],ink,6)
    if expression=='blink':
        line([(225,285),(241,291),(257,285)],ink,5);line([(344,285),(359,291),(375,285)],ink,5)
    else:
        ellipse((228,274,255,304),'#FAF5E9');ellipse((345,274,372,304),'#FAF5E9')
        ellipse((238,276,254,302),ink);ellipse((347,276,363,302),ink)
        ellipse((244,278,250,284),'#FFFFFF');ellipse((353,278,359,284),'#FFFFFF')
    line([(300,288),(293,321),(306,326)],'#B67B66' if nova else '#95563F',4)
    if expression=='talk':
        ellipse((273,344,328,381),ink);rect((278,346,324,355),3,'#FFF4E4');ellipse((285,369,319,382),'#D98279')
    else:
        d.arc((273*S,328*S,329*S,366*S),15,165,fill=ink,width=5*S)
    # Jacket accent and restrained rim light.
    line([(126,553),(112,632)],'#D9CEE9' if nova else '#A1CAC0',6)
    im.resize((600,740),Image.Resampling.LANCZOS).save(ROOT/f'{name}-{expression}.png',optimize=True)

def make_robot(name, expression):
    im = Image.new('RGBA', (600*S, 740*S))
    d = ImageDraw.Draw(im)
    ink = '#203C44'; cream = '#F3EBD6'; mint = '#A4D5C1'; gold = '#E5B967'
    def rect(box, radius, fill, outline=None, width=1):
        d.rounded_rectangle(tuple(round(v*S) for v in box), radius*S, fill, outline, width*S)
    def ellipse(box, fill, outline=None, width=1):
        d.ellipse(tuple(round(v*S) for v in box), fill, outline, width*S)
    def line(points, fill, width=1):
        d.line([(round(x*S),round(y*S)) for x,y in points], fill, width*S, joint='curve')
    def poly(points, fill, outline=ink, width=5):
        points = [(round(x*S),round(y*S)) for x,y in points]
        d.polygon(points, fill)
        if outline: d.line(points+[points[0]],outline,width*S,joint='curve')
    def gear(cx, cy, radius):
        points=[]
        for i in range(48):
            angle=math.tau*i/48
            r=radius*(1 if i%4 in (1,2) else .82)
            points.append((cx+math.cos(angle)*r,cy+math.sin(angle)*r))
        poly(points,gold,ink,5)
        ellipse((cx-radius*.42,cy-radius*.42,cx+radius*.42,cy+radius*.42),'#A4644C',ink,4)
        ellipse((cx-radius*.13,cy-radius*.13,cx+radius*.13,cy+radius*.13),gold)
    cog = name == 'cog'
    if cog:
        copper='#D89065'; shade='#AB654C'
        # Chunky brass joints and workshop apron create a small tactile robot.
        rect((254,376,347,449),18,shade,ink,6)
        rect((153,427,447,735),67,copper,ink,7)
        rect((98,451,185,655),36,shade,ink,6);rect((416,451,503,655),36,shade,ink,6)
        ellipse((87,614,188,715),copper,ink,6);ellipse((414,614,515,715),copper,ink,6)
        line([(123,651),(145,665),(155,649)],ink,5);line([(448,649),(458,665),(480,651)],ink,5)
        poly([(207,441),(240,451),(240,489),(360,489),(360,451),(393,441),(419,731),(182,731)],'#447A73',ink,6)
        rect((238,547,361,643),18,cream,ink,5)
        gear(300,593,30)
        for x in (223,379):ellipse((x-8,490,x+8,506),gold,ink,3)
        # Asymmetric antenna and side cogs identify Cog at silhouette scale.
        line([(298,120),(298,69),(335,50)],ink,11)
        ellipse((321,31,357,67),mint,ink,5)
        gear(151,248,66);gear(449,248,66)
        rect((153,113,447,408),70,copper,ink,7)
        rect((174,137,426,371),55,'#E6AB7B')
        rect((188,180,412,316),36,ink)
        line([(201,157),(260,150)],'#F5CE9D',7)
        for x in (225,375):ellipse((x-6,344,x+6,356),shade)
        # Soft mint scarf, an orange stitched corner and a tiny chest tool.
        poly([(210,401),(296,420),(390,401),(380,445),(296,459),(220,443)],mint,ink,5)
        poly([(331,449),(374,440),(405,506),(361,498)],'#7FBAA8',ink,5)
        eye_y=231;eyes=(241,359)
        if expression=='blink':
            for x in eyes:line([(x-18,eye_y),(x+18,eye_y)],mint,7)
        else:
            for x in eyes:rect((x-14,eye_y-19,x+14,eye_y+19),10,mint)
        if expression=='talk':
            rect((276,273,324,296),8,mint)
            line([(285,278),(285,291)],ink,3);line([(299,278),(299,291)],ink,3);line([(313,278),(313,291)],ink,3)
        else:line([(276,278),(286,285),(311,285),(322,278)],mint,5)
    else:
        teal='#649C96'; shadow='#3D7371'
        # Long faceted silhouette and a floating crystal antenna are Axiom's motif.
        poly([(300,20),(324,51),(300,82),(276,51)],gold,ink,5)
        line([(300,79),(300,123)],ink,6)
        rect((264,363,336,430),14,shadow,ink,5)
        poly([(204,410),(300,435),(396,410),(437,530),(404,739),(196,739),(163,530)],cream,ink,7)
        poly([(196,421),(142,454),(96,663),(161,700),(215,526)],teal,ink,6)
        poly([(404,421),(458,454),(504,663),(439,700),(385,526)],teal,ink,6)
        poly([(109,621),(168,642),(153,716),(107,724),(82,690)],cream,ink,5)
        poly([(491,621),(432,642),(447,716),(493,724),(518,690)],cream,ink,5)
        poly([(204,417),(300,462),(396,417),(375,496),(300,528),(225,496)],teal,ink,5)
        line([(300,528),(300,730)],'#C6CABC',5)
        poly([(300,548),(336,584),(300,620),(264,584)],gold,ink,4)
        poly([(300,562),(321,584),(300,605),(279,584)],cream,ink,3)
        line([(209,672),(256,672)],teal,6);line([(344,672),(391,672)],teal,6)
        # Head is a beveled ivory hexagon, with a dark continuous display.
        poly([(228,102),(372,102),(435,163),(420,305),(353,379),(247,379),(180,305),(165,163)],cream,ink,7)
        poly([(174,165),(211,185),(218,287),(250,331),(247,367),(188,302)],'#D4D8C8',None)
        poly([(228,102),(372,102),(409,139),(350,132),(249,132),(191,139)],'#FFFBEA',None)
        poly([(204,174),(396,174),(390,287),(358,320),(242,320),(210,287)],ink,ink,5)
        poly([(300,125),(318,143),(300,161),(282,143)],teal,None)
        if expression=='blink':
            for x in (247,353):line([(x-17,230),(x+17,230)],mint,6)
        else:
            for x in (247,353):
                ellipse((x-16,211,x+16,245),mint)
                ellipse((x-6,217,x+6,239),ink)
        if expression=='talk':
            for i,height in enumerate((9,19,27,19,9)):
                x=278+i*11
                rect((x,278-height/2,x+6,278+height/2),3,gold)
        else:line([(279,279),(321,279)],gold,5)
        line([(268,349),(332,349)],shadow,5)
    im.resize((600,740),Image.Resampling.LANCZOS).save(ROOT/f'{name}-{expression}.png',optimize=True)


if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--pair',choices=('all','cog-axiom','nova-atlas'),default='all')
    args=parser.parse_args()
    names=('cog','axiom') if args.pair=='cog-axiom' else ('nova','atlas') if args.pair=='nova-atlas' else ('cog','axiom','nova','atlas')
    for name in names:
        for expression in ('idle','talk','blink'):
            (make_robot if name in ('cog','axiom') else make)(name,expression)
