import collections,re,gzip,sys
# usage: python3 summarize.py counts-20260921.tsv.gz
rows=[l.rstrip('\n').split('\t') for l in gzip.open(sys.argv[1],'rt')]
N=0; ckind=collections.Counter()
ops=collections.defaultdict(collections.Counter); opk=collections.defaultdict(collections.Counter)
for p,a,k,c in rows:
    c=int(c)
    if p=='census': N+=c; ckind[k]+=c
    else: ops[p][a]+=c; opk[(p,a)][k]+=c
NONNODE={'Symbol','FlowNode','FlowList','FlowLabel','NodeFactory','NodeVisitor','NodeVisitorHooks','SourceFile','Diagnostic','FlowReduceLabelData','FlowSwitchClauseData','SymbolTable','CompositeBase','NodeFactoryHooks','CommentRange','CommentDirective','FileReference','PragmaSpecification','Pragma','PragmaArgument'}
POLY={'Name','Text','Expression','Type','Initializer','Body','Modifiers','ModifierFlags','ModifierNodes','TypeParameters','Parameters','Arguments','TypeArguments','Members','Statements','Elements','Label','Statement','PropertyName','QuestionToken','PostfixToken','TagName','Children','Properties','ArgumentList','TypeParameterList','ParameterList','StatementList','MemberList','ElementList','PropertyList','Decorators','QuestionDotToken','TypeArgumentList','Attributes','ClassName','IsTypeOnly','Comments','CommentList','ModuleSpecifier','ImportClause','Literal','PropertyNameOrName','DefaultType','Constraint','Operand','Operator','Left','Right'}
SIDE={'Symbol','LocalSymbol','Locals','DeclarationData','ExportableData','LocalsContainerData','FlowNodeData','FunctionLikeData','BodyData','ClassLikeData','SubtreeFacts','propagateSubtreeFacts','computeSubtreeFacts','ContextualFlowNode','EmitNode'}
def cls(a):
    t,rest=a[:2],a[2:]
    if t=='N:':
        f=rest.split('.')[1].split('@')[0]
        return '1 header:'+f
    if t=='S:':
        s=rest.split('.')[0]
        if s in NONNODE: return 'x non-node struct'
        if s in ('NodeList','ModifierList'): return '4 list field'
        if s.endswith('Base') or s=='NodeDefault': return '5 side/base field'
        return '2 typed field'
    if t=='M:':
        m=rest[5:-2]
        if m.startswith('As'): return '2 AsXxx cast'
        if m in SIDE: return '5 side method'
        if m in POLY: return '3 poly method'
        if m in ('Pos','End'): return '1 header:PosEnd()'
        if m in ('ForEachChild','VisitEachChild','IterChildren','Clone'): return '6 walk'
        return '7 other Node method'
    if t=='F:':
        f=rest[4:-2]
        if f=='GetNodeId': return '5 side method'
        if re.match(r'Is[A-Z]',f): return '8 ast.IsXxx()'
        return '9 other ast func'
phases=['parse','bind','check','emit']
tab=collections.defaultdict(collections.Counter)
for p in phases:
    for a,c in ops[p].items(): tab[cls(a)][p]+=c
print('class'.ljust(24),*[p.rjust(14) for p in phases])
for k in sorted(tab): print(k.ljust(24),*[f'{tab[k][p]/N:14.2f}' for p in phases])
print('TOTAL(excl x)'.ljust(24),*[f'{sum(tab[k][p] for k in tab if not k.startswith("x"))/N:14.2f}' for p in phases])
def top(pred,n=25,title=''):
    print('\n##',title)
    agg=collections.defaultdict(collections.Counter)
    for p in phases:
        for a,c in ops[p].items():
            if pred(a): agg[a][p]+=c
    for a,cs in sorted(agg.items(), key=lambda x:-x[1]['check']-x[1]['bind']-x[1]['emit'])[:n]:
        print(f'  {a:50s}',*[f'{cs[p]:>12,}' for p in phases])
top(lambda a: cls(a)=='3 poly method',20,'poly methods')
top(lambda a: cls(a)=='5 side method',14,'side methods')
top(lambda a: cls(a)=='2 AsXxx cast',15,'casts')
top(lambda a: cls(a)=='2 typed field' and a.endswith('@out'),30,'typed field reads by consumers')
top(lambda a: cls(a)=='9 other ast func',25,'other ast funcs')
top(lambda a: cls(a)=='7 other Node method',12,'other Node methods')
for m in ['M:Node.Name()','M:Node.Expression()','M:Node.Text()','M:Node.Symbol()','M:Node.Locals()','M:Node.ModifierFlags()','N:Node.Kind@out','N:Node.Parent@ast','N:Node.Parent@out','F:ast.GetSourceFileOfNode()']:
    d=opk[('check',m)]; t=sum(d.values())
    print('\n',m,'check by kind:',[(k[4:],round(100*c/t,1)) for k,c in d.most_common(7)])
