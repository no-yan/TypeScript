import sys,re,statistics as st,collections
d=collections.defaultdict(lambda: collections.defaultdict(list))
for l in open(sys.argv[1]):
    if not l.startswith('Benchmark'): continue
    f=l.split('\t'); name=re.sub(r'-\d+\s*$','',f[0].strip()).replace('BenchmarkStoreExp','')
    for x in f[2:]:
        v,u=x.split(); d[name][u].append(float(v))
def med(n,u): return st.median(d[n][u])
def spread(n,u): v=d[n][u]; return 100*(max(v)-min(v))/st.median(v)
print("== Walk (per visit)")
for n in d:
    if not n.startswith('Walk'): continue
    c=med(n,'visits/op'); print(f"{n:32} inst/visit {med(n,'inst/op')/c:7.2f} (spread {spread(n,'inst/op'):.2f}%)  cyc/visit {med(n,'cycles/op')/c:6.2f} ({spread(n,'cycles/op'):.1f}%)  IPC {med(n,'IPC'):.2f}")
print("== Access")
A='AccessKPC/'
bi={s:med(A+'baseline/'+s,'inst/op')/med(A+'baseline/'+s,'ops/op') for s in('pointer','store')}
bc={s:med(A+'baseline/'+s,'cycles/op')/med(A+'baseline/'+s,'ops/op') for s in('pointer','store')}
print("baseline per elem: inst",bi,"cycles",bc)
print(f"{'op':24}{'ops':>8} {'raw i/acc':>10} {'net i/acc':>10} {'raw c/acc':>10} {'net c/acc':>10} {'IPC':>5} {'spr i%':>7} {'spr c%':>7}  detail")
for n in d:
    if not n.startswith(A): continue
    side=n.rsplit('/',1)[1]; ops=med(n,'ops/op'); i=med(n,'inst/op')/ops; c=med(n,'cycles/op')/ops
    det=[(u,med(n,u)) for u in d[n] if u.endswith('/op') and u not in('ns/op','B/op','allocs/op','inst/op','cycles/op','ops/op')]
    ds=' '.join(f"{u}={int(v)} net_i/{u[:-3]}={(i-bi[side])*ops/v:.2f} net_c={(c-bc[side])*ops/v:.2f}" for u,v in det if v)
    print(f"{n[len(A):]:24}{int(ops):8d} {i:10.2f} {i-bi[side]:10.2f} {c:10.2f} {c-bc[side]:10.2f} {med(n,'IPC'):5.2f} {spread(n,'inst/op'):7.2f} {spread(n,'cycles/op'):7.1f}  {ds}")
