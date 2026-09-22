import sys,re,statistics as st,collections
unit=sys.argv[2]  # ns/visit or ns/access
d=collections.defaultdict(list); extra={}
for l in open(sys.argv[1]):
    if not l.startswith('Benchmark'): continue
    f=l.split('\t'); name=re.sub(r'-\d+\s*$','',f[0].strip()).split('/',1)[1]
    m={}
    for x in f[2:]:
        v,u=x.split(); m[u]=float(v)
    d[name].append(m[unit]); extra[name]=m
print(f"{'name':40} {'median':>8} {'spread%':>8}  ops  detail")
for k,v in d.items():
    med=st.median(v); e=extra[k]
    det=[f"{u}={int(x)}" for u,x in e.items() if u.endswith('/op') and u not in('ns/op','B/op','allocs/op')]
    print(f"{k:40} {med:8.3f} {100*(max(v)-min(v))/med:8.1f}  {' '.join(det)}")
