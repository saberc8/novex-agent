/** Synthetic enterprise fixtures, authored only with the bundled artifact-tool. */
import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';
import { Workbook, SpreadsheetFile } from '@oai/artifact-tool';

const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'../..');
const out=path.join(root,'fixtures/enterprise');
const qa=path.join(root,'.local/enterprise-artifacts');
await fs.mkdir(out,{recursive:true});await fs.mkdir(qa,{recursive:true});
const categories=['紧固件','轴承','电动工具','管件','阀门','劳保用品','测量工具','传动件'];
const anchors={1:{name:'不锈钢六角螺栓 M8×30',sale:68,cost:38,category:'紧固件'},2:{name:'工业轴承 6204-2RS',sale:29,cost:16,category:'轴承'},3:{name:'电动扭矩扳手 18V',sale:899,cost:520,category:'电动工具'}};
function product(n){const sale=Math.round((15+(n%500)*1.75)*100)/100;return {sku:`HW-${String(n).padStart(6,'0')}`,name:`工业备件-${String(n).padStart(4,'0')}`,sale,cost:Math.round(sale*.58*100)/100,category:categories[(n-1)%8],...anchors[n],warranty:12};}
function style(sheet,range,cols){
  sheet.showGridLines=false;sheet.freezePanes.freezeRows(1);
  sheet.getRange(range).format.font={name:'Arial',size:10,color:'#1F2937'};
  sheet.getRange(range).format.rowHeight=20;
  const end=String.fromCharCode(64+cols);
  sheet.getRange(`A1:${end}1`).format={fill:'#21354B',font:{name:'Arial',size:10,bold:true,color:'#FFFFFF'},rowHeight:28,horizontalAlignment:'center',verticalAlignment:'center'};
  sheet.getRange(range).format.verticalAlignment='center';
  for(let i=0;i<cols;i++)sheet.getRange(`${String.fromCharCode(65+i)}1:${String.fromCharCode(65+i)}2`).format.columnWidth=22;
}
async function verifyAndExport(book,name,ranges,extraChecks=[]){
  book.recalculate();
  const inspections=[];
  for(const [sheet,range] of ranges){
    const inspect=await book.inspect({kind:'table',range:`'${sheet}'!${range}`,include:'values,formulas',tableMaxRows:10,tableMaxCols:10,maxChars:4000});inspections.push(inspect.ndjson);
    const png=await book.render({sheetName:sheet,range,scale:1.3,format:'png'});
    await fs.writeFile(path.join(qa,`${name}-${sheet}.png`),new Uint8Array(await png.arrayBuffer()));
  }
  const errors=await book.inspect({kind:'match',searchTerm:'#REF!|#DIV/0!|#VALUE!|#NAME\\?|#N/A|#NUM!|#NULL!|#SPILL!|#CALC!',options:{useRegex:true,maxResults:20},maxChars:3000});
  const exportFile=await SpreadsheetFile.exportXlsx(book);await exportFile.save(path.join(out,name+'.xlsx'));
  // The exporter emits a full internal inspection sidecar; bounded QA above is sufficient.
  await fs.rm(path.join(out,name+'.xlsx.inspect.ndjson'),{force:true});
  await fs.writeFile(path.join(qa,name+'-qa.json'),JSON.stringify({inspections,errors:errors.ndjson,extraChecks},null,2));
  console.log(JSON.stringify({export:name,bytes:(await fs.stat(path.join(out,name+'.xlsx'))).size}));
}

// 2,500 SC001 products x 20 historical quotation months. No other tenant's rows.
const catalog=Workbook.create();
const items=catalog.worksheets.add('Catalog');
const after=catalog.worksheets.add('AfterSales');
items.getRange('A1:I1').values=[['sku','quote_month','product_name','category','sale_price_cny','warranty_months','effective_date','expires_date','source_type']];
const catalogRows=[];
for(let month=0;month<20;month++)for(let n=1;n<=2500;n++){
  const p=product(n),date=new Date(Date.UTC(2025,3+month,1)),end=new Date(Date.UTC(2025,4+month,0));
  catalogRows.push([p.sku,date.toISOString().slice(0,7),p.name,p.category,Math.round((p.sale-(19-month)*.1)*100)/100,p.warranty,date,end,month===19?'current':'historical']);
}
items.getRange('A2:I50001').values=catalogRows;
style(items,'A1:I50001',9);items.getRange('C1:C2').format.columnWidth=34;items.getRange('E2:E50001').setNumberFormat('#,##0.00');items.getRange('F2:F50001').setNumberFormat('0');items.getRange('G2:H50001').setNumberFormat('yyyy-mm-dd');
items.tables.add('A1:I50001',true,'CatalogRows');
after.getRange('A1:F9').values=[['policy_id','product_scope','warranty_months','return_days','defect_report_hours','condition'],...categories.map((category,i)=>[`AS-${String(i+1).padStart(3,'0')}`,category,12,15,48,'质量缺陷凭订单和检测记录受理'])];
style(after,'A1:F9',6);after.getRange('F1:F2').format.columnWidth=39;after.tables.add('A1:F9',true,'AfterSalesRules');
const samples=[1,2500,25001,47501,49999,50000].map(index=>({excelRow:index+1,values:catalogRows[index-1].map(v=>v instanceof Date?v.toISOString().slice(0,10):v)}));
await verifyAndExport(catalog,'hardware-catalog-50000',[['Catalog','A1:I10'],['AfterSales','A1:F9']],[{rowCount:catalogRows.length,samples}]);

const procurement=Workbook.create();
const price=procurement.worksheets.add('Procurement');
const terms=procurement.worksheets.add('SupplierTerms');
price.getRange('A1:H1').values=[['sku','product_name','base_price_cny','order_qty','vat_rate','taxed_unit_cny','line_total_cny','valid_until']];
const priceRows=Array.from({length:24},(_,i)=>{const p=product(i+1);return [p.sku,p.name,p.cost,i===0?120:i===1?200:i===2?5:10+i,.13,null,null,new Date('2026-12-31T00:00:00Z')];});
price.getRange('A2:H25').values=priceRows;
price.getRange('F2:G2').formulas=[['=ROUND(C2*(1+E2),2)','=ROUND(D2*F2,2)']];price.getRange('F2:G25').fillDown();
style(price,'A1:H25',8);price.getRange('B1:B2').format.columnWidth=34;price.getRange('C2:C25').setNumberFormat('#,##0.00');price.getRange('D2:D25').setNumberFormat('#,##0');price.getRange('E2:E25').setNumberFormat('0.0%');price.getRange('F2:G25').setNumberFormat('#,##0.00');price.getRange('H2:H25').setNumberFormat('yyyy-mm-dd');price.tables.add('A1:H25',true,'ProcurementRows');
terms.getRange('A1:D5').values=[['term_id','rule','value','unit'],['PT-001','SC001采购报价有效期',30,'天'],['PT-002','质量异议提交期限',48,'小时'],['PT-003','已批准采购单付款账期',30,'天'],['PT-004','未授权采购账号可读取的私有报价行数',0,'行']];
style(terms,'A1:D5',4);terms.getRange('B1:B2').format.columnWidth=58;terms.tables.add('A1:D5',true,'SupplierRules');
procurement.recalculate();
const before=price.getRange('G2').values[0][0];if(Math.abs(Number(before)-5152.8)>.001)throw new Error(`Formula cache mismatch ${before}`);
price.getRange('D2').values=[[121]];procurement.recalculate();const changed=price.getRange('G2').values[0][0];if(Math.abs(Number(changed)-5195.74)>.001)throw new Error('Formula recalculation did not follow quantity change');
price.getRange('D2').values=[[120]];
await verifyAndExport(procurement,'hardware-procurement-private',[['Procurement','A1:H10'],['SupplierTerms','A1:D5']],[{cell:'Procurement!G2',expected:5152.8,quantityChange121Expected:5195.74,observedChanged:changed,restored:true}]);

const groups={
 HR:[
 ['工资发放日','每月10日发放上月工资。遇企业结算日暂停时由HR公告具体日期，不能自行推算。','我10月工资什么时候发？','按本制度通常在11月10日发放，金额必须实时查询工资系统。'],
 ['工资核对窗口','工资到账后5个工作日内可提交核对单，材料包含工资月份和异议字段。','工资不对如何处理？','在到账后5个工作日内向HR提交核对单，禁止向无权限人员发送同事工资。'],
 ['年假申请','入职满一年且系统确认资格的员工，年度公司年假额度为7天。至少提前3个工作日申请。','入职满一年有多少年假？','公司年假额度为7天，申请需提前3个工作日，余额以HR系统为准。'],
 ['报销申请时限','一般差旅票据应在返程后15个自然日内提交。超期需部门负责人说明。','出差回来何时报销？','返程后15个自然日内提交票据与行程，不在本制度推定金额上限。'],
 ['补签申请','忘记考勤可在事件发生后2个工作日内申请补签，每月最多3次。','忘记打卡怎么办？','2个工作日内提交补签说明，每月最多3次。'],
 ['培训登记','必修培训应在入职后10个工作日内完成并登记，记录保留24个月。','新员工培训期限？','入职后10个工作日内完成，培训记录保留24个月。'],
 ['工牌补办','工牌遗失应在发现后4小时内报备并停用，补办审批通过后2个工作日领取。','工牌丢了怎么办？','4小时内报备停用，审批后2个工作日领取新卡。'],
 ['办公设备借用','借用电脑需登记资产编号和归还日期，最长借用30天，延期需再次审批。','能借电脑多久？','单次最长30天，延期须再次审批。'],
 ['试用期资料','入职资料清单在到岗后3个工作日内补齐；本文件不规定个人试用期长度。','试用期到底多长？','个人试用期须查授权合同，本文件仅规定资料补齐3个工作日。'],
 ['离职交接','交接单至少包含资产、账号、项目、费用4类事项，全部责任人确认后归档。','离职交接查什么？','核对资产、账号、项目、费用四类事项，不据此承诺离职审批日期。'],
 ['加班记录','已审批加班应在次月3日前完成记录确认；工资计算以有效记录与薪酬规则为准。','加班记录何时确认？','次月3日前确认，不能仅凭聊天声明计算加班费。'],
 ['个人信息更正','本人可申请更正联系方式，HR在2个工作日内核验；身份证件不得放入公共知识库。','联系方式错误怎么改？','提交本人更正申请，HR在2个工作日内核验。'],
 ['会议室预约','预约最长4小时，开始后15分钟无人使用可释放；涉及客户的会议需标注接待人。','会议室没到场会怎样？','开始15分钟无人使用可释放，单次最长4小时。'],
 ['访客登记','访客通行默认限当日，提前1个工作日提交接待人和访问区域。','访客证有效多久？','默认当日有效，申请需提前1个工作日。'],
 ['账号离岗停用','离岗确认后账号按工单在当日停用，业务记录由授权负责人接管。','离职员工还能看工资吗？','需依据停用状态与合法保留访问流程，不能沿用旧会话绕过权限。'],
 ['敏感薪资沟通','员工只能查询获授权的工资记录，部门负责人权限不自动包含奖金明细与银行卡。','经理能看所有奖金吗？','不能；奖金和银行卡必须另有字段授权，前端和聊天都执行相同策略。']
 ],
 WMS:[
 ['可用库存计算','可用库存=账面库存减已预留数量。负数必须标记异常，不能把账面库存当成可发货量。','如何算能发多少？','实时查询账面库存与预留数再相减，不能用历史文档代替库存。'],
 ['当日发货截单','工作日16:00前完成审核、付款核验与库存分配的普通订单可进入当日发货队列。','15点下单一定今天发吗？','还必须完成审核、付款核验和库存分配；仅下单时间不足以承诺发货。'],
 ['缺货订单处理','可用库存不足时记录缺口，客户同意后允许拆单。未经同意不能擅自改为部分发货。','库存不够怎么处理？','计算缺口并征求拆单同意，补货日期只使用已确认采购数据。'],
 ['仓库授权','仓库W1和W2分别授权；能查看W1不能推出W2权限。同名仓库跨租户也分别隔离。','能查深圳所有库存吗？','只有身份映射授予的仓库可查，不得通过总量差值推断其他仓库。'],
 ['收货复核','收货清点按采购单逐行核验SKU、数量和外包装，差异应在2小时内登记。','收货差异何时报？','发现后2小时内登记SKU、数量与图片证据。'],
 ['上架要求','完成质检后4小时内上架。未通过质检的批次进入隔离库位，不能计入可售库存。','待检品可以卖吗？','不可以，未通过质检的批次不得计入可售库存。'],
 ['拣货复核','普通订单至少进行一次SKU扫码与数量复核，错码不得人工忽略。','扫码不符能直接发吗？','不能；应退回异常队列复核。'],
 ['盘点差异','差异达到5件或账面金额500元时需双人复核，未核准前不修改可用库存。','盘亏怎么处理？','达到5件或500元需双人复核，以核准调整单变更库存。'],
 ['预留超时','未付款普通订单的库存预留默认2小时；续留必须生成新的审批记录。','预留多久释放？','默认2小时，须检查订单类型与已审批续留记录。'],
 ['调拨确认','调拨必须同时记录发出仓和接收仓。接收确认前物料记为在途，不直接增加可售数。','在途算现货吗？','不算；接收确认后才更新可用库存。'],
 ['发货包装','轴承使用防锈袋与缓冲材料，外箱注明SKU和件数，禁止遮挡追溯标签。','轴承怎么包装？','防锈袋加缓冲材料，保留追溯标签并注明SKU和件数。'],
 ['物流签收差异','破损或短少应在签收后48小时内登记，并保留外箱与运单照片。','签收少件怎么办？','48小时内登记并提交外箱、运单及数量证据。'],
 ['出库追溯','每次出库必须关联订单号、SKU、仓库、操作时间。不能只凭聊天记录视为已发货。','客服说发了就算发货吗？','应核对有效出库记录和运单，聊天描述不能替代。'],
 ['退货入库','退货到仓后1个工作日内登记，质检合格后才转为可售库存。','退货到了库存何时加？','先1个工作日内登记，质检合格后再转可售。'],
 ['危险品隔离','本目录不经营危险化学品。描述不明的液体先隔离并由合规人员确认，不创建普通出库单。','未知液体能普通快递发吗？','不能，先隔离确认，不能套用普通五金发货规则。'],
 ['库存更新时间','报表为查询时点快照；多轮对话中再次承诺库存前必须重新查询，旧答案不能充当库存锁定。','刚才说有货现在还能保证吗？','需要重新查询并以订单预留成功结果为准。']
 ],
 AS:[
 ['标准保修','本目录所有SKU标准保修为12个月，从有效签收日期起算，质量缺陷需凭订单和检测记录受理。','HW-000002保修多久？','工业轴承6204-2RS标准保修12个月，需核对有效签收日期和缺陷证据。'],
 ['退货期限','标准商品质量问题退货申请窗口为签收后15个自然日。拆封、使用损伤和定制品另行核验。','签收10天质量问题可申请吗？','仍在15天申请窗口内，需核验缺陷与商品状态后受理。'],
 ['质量证据','售后申请必须提供订单号、SKU、问题描述、现场照片四项信息。','申请保修要什么？','提供订单号、SKU、问题描述和现场照片。'],
 ['初次响应','工作时间提交的完整售后材料应在4小时内首次响应，首次响应不等于完成维修。','多久能修好？','本条只承诺4小时首次响应，维修完成时间需依据检测。'],
 ['检测时限','售后件到达检测中心后2个工作日内给出初步检测结果，复杂故障单独说明。','寄到后多久检测？','通常2个工作日给出初步结果，复杂故障另行说明。'],
 ['非质量退换','非质量退换必须保持商品完整并经客服确认；运费通常由申请方承担。','不喜欢能退吗？','先核验商品状态与审批条件，不能只凭偏好承诺无条件退款。'],
 ['质量退回运费','确认属于质量缺陷且获售后授权的退回件，由企业承担约定的标准运输费用。','质量退货谁付运费？','确认质量缺陷且获授权后由企业承担约定标准费用。'],
 ['退款到账','仓库验收与财务审核完成后3个工作日内发起退款。银行实际到账以渠道为准。','退货后当天到账吗？','不保证；完成验收与审核后3个工作日内发起退款。'],
 ['换货型号','换货必须核对SKU、规格与数量；不同型号替换需客户明确同意。','轴承没货换别的可以吗？','必须客户明确同意并核对规格，不能自行替换。'],
 ['易损件范围','正常磨损、误用、超负荷和未经授权拆机不自动属于保修质量缺陷，需检测认定。','过载损坏保修吗？','不能直接承诺，按检测认定是否为质量缺陷。'],
 ['售后进度','售后状态以服务工单为准，客服只读权限不能改为已解决或已退款。','帮我标记已退款？','需要有权限的业务动作与财务结果，查询不能改变状态。'],
 ['缺件补发','确认缺件后核对原订单和实际数量，补发单必须引用原订单号，避免重复补发。','少一件直接补两件？','按确认短少数量补发，补发单关联原订单。'],
 ['收件地址更改','售后补发地址变更需验证原订单联系人，出库后变更需物流确认。','能改发给新地址吗？','先验证原订单联系人，已出库须等待物流确认。'],
 ['维修后保修','维修不默认重置整机保修起算日；更换部件的承诺须依据具体工单条款。','维修后重新保12个月吗？','不能一概而论，核对具体更换部件和工单承诺。'],
 ['服务升级','同一问题连续2次未解决可提交升级工单，升级不自动授权赔偿金额。','投诉后赔多少钱？','需审批后的方案，本制度只提供连续2次未解决的升级条件。'],
 ['未知产品咨询','未在目录、订单或有效文件中找到的型号应明确回复无法确认，不能编造上市日期、价格或保修条款。','未知型号下月上市吗？','没有可靠证据时回复无法确认，不用近似型号代替。']
 ],
 PRODUCT:[
 ['螺栓识别','HW-000001为不锈钢六角螺栓M8×30，销售单位为包，每包100枚。当前售价从产品库查询，历史报价按quote_month字段选择。','M8×30是什么SKU？','HW-000001，不得把历史报价直接当作当前成交价。'],
 ['轴承识别','HW-000002为工业轴承6204-2RS，2RS表示双侧密封规格标记。替换前应核验尺寸与使用条件。','6204-2RS对应哪个商品？','HW-000002，替代方案需核验规格。'],
 ['扭矩扳手识别','HW-000003为电动扭矩扳手18V。本文件不包含未经验证的最大扭矩或续航参数。','扭矩最大多少？','此文件没有最大扭矩参数，须查有效技术规格书。'],
 ['SKU精确匹配','SKU采用HW-加六位数字。前导零必须保留，不能把名称相似视为同一产品。','HW-1等于HW-000001吗？','需确认标准SKU后查询，不自行合并不一致标识。'],
 ['报价月份选择','Catalog每个SKU有20条月度报价，覆盖2025年4月至2026年11月。当前报价为2026-11。','为什么同SKU有多行？','不同quote_month表示不同月度报价，查询应明确月份。'],
 ['历史报价有效期','每条报价的effective_date与expires_date界定该自然月。历史快照不表示现价，也不锁定库存。','去年价格还能用吗？','只能作为历史查询结果，当前成交需查现行报价与审批。'],
 ['计价币种','目录及采购表中的金额均为人民币元，带cny字段。不得将单价误读为千元或美元。','表里68是什么币种？','人民币元；具体是否含税看字段说明和业务单据。'],
 ['目录税口径','Catalog.sale_price_cny表示销售含税展示价；Procurement.base_price_cny为供应商未税基价。两者不能直接作含税差额比较。','采购38和销售68能直接算毛利吗？','先统一税口径与其他成本后再计算，本目录不提供完整毛利口径。'],
 ['停产替代','只有经审核的替代清单才可推荐停产替代型号。本测试资料未提供替代清单。','某产品停产可用什么替代？','没有审核清单时不能凭相似名称直接推荐。'],
 ['规格单位','长度按毫米记录，电压按伏特记录。报价数量单位应与订单单位一致。','30是厘米吗？','M8×30中的长度标记按毫米，不能自行改成厘米。'],
 ['批次追溯','批次号、SKU与供应商共同用于追溯；只提供SKU不足以确定生产批次。','知道SKU能知道生产日期吗？','还需批次或有效出入库记录，不能从SKU数字推断。'],
 ['产品图片用途','展示图片仅用于识别，不证明尺寸精度、材质证书或库存数量。','图片看着一样能确认通用吗？','不能，应查询正式参数和适配记录。'],
 ['参数缺失处理','缺失参数必须标记未提供，不以零代替。零库存与未获授权查看库存是不同状态。','没显示库存是不是0？','不一定，要区分真实零、数据缺失与权限不可见。'],
 ['报价审批','非标准折扣必须关联已审批报价单，客服查询权限不能授予任何折扣。','帮我打八折？','需有效报价审批，不能仅凭对话修改价格。'],
 ['产品目录版本','企业演示数据版本为2026-11-06。所有记录为合成数据，用于隔离、检索与正确性验证。','这些是真客户数据吗？','不是，均为合成企业测试资料。'],
 ['检索引用','Excel回答应注明文件、工作表及行或单元格；Markdown回答注明文档编号和相关标题。','报价来源怎么标？','保留文件名、Catalog工作表以及对应记录行，不能只写网上查到。']
 ],
 PROC:[
 ['采购价格保密','采购基价、供应商折扣与账期仅对明确授权角色开放，不随普通产品售价权限自动开放。','员工能看采购底价吗？','不能，必须有独立授权。'],
 ['采购税额','采购表税率为13%。含税单价=未税基价×1.13并四舍五入至分，行金额=数量×含税单价。','税额怎么算？','使用表中vat_rate，先算含税单价再按数量计算。'],
 ['螺栓采购批量','当前私有报价HW-000001未税基价38元/包，数量120包，每包100枚，税率13%，含税单价42.94元/包，行金额5152.80元。','120包螺栓含税金额？','5152.80元，须引用私有采购表并验证访问权限。'],
 ['轴承采购批量','当前私有报价HW-000002未税基价16元，数量200，税率13%，含税单价18.08元，行金额3616元。','200件轴承含税金额？','3616元，仅限授权采购资料使用。'],
 ['扳手采购批量','当前私有报价HW-000003未税基价520元，数量5，税率13%，含税单价587.60元，行金额2938元。','5件扳手含税金额？','2938元，基于本批次报价。'],
 ['供应商报价期限','供应商报价默认有效30天；采购表另列valid_until时以明确日期为准。','报价多久有效？','默认30天，有valid_until则用该日期。'],
 ['对公付款账期','已批准采购单验收和对账完成后按30天账期结算，争议款暂停并记录原因。','验收就能马上付款吗？','仍需对账与批准，按30天账期执行。'],
 ['采购单变更','数量、价格或收货仓变更均需保留原版本和审批记录，禁止覆盖原订单。','能直接改原采购价吗？','不能，应创建变更记录并审批。'],
 ['供应商准入','新增供应商须完成资质、收款账户和质量能力三项核验，不得通过聊天直接准入。','新供应商怎么启用？','三项核验通过后按批准流程启用。'],
 ['最小采购量','未列最小采购量的SKU不得推定为1或0。询价需要明确包装单位和最小量。','最少买几件？','查有效报价条款；未列明则明确待确认。'],
 ['采购交期','仅使用供应商已确认交期，预计时间与承诺日期分别记录。缺失交期不能承诺当天补货。','今天缺货明天一定到吗？','没有已确认交期不能保证。'],
 ['紧急采购','紧急采购仍需价格、数量、用途和批准人四项记录，紧急程度不能绕过租户权限。','急单可以不审批吗？','不可以，四项记录与批准仍必需。'],
 ['供应商质保','对外客户保修与供应商赔付条款分别管理，不得将内部供应商折扣公开给客户。','客户能看供应商赔付价吗？','不随售后查询权限开放。'],
 ['采购对账','按采购单、收货单和发票三方核对。数量或金额不一致时先暂停差异部分付款。','发票比收货多怎么办？','暂停差异部分并核对三单。'],
 ['价格异常复核','同SKU新报价比上一有效报价变化超过10%时需人工复核，未通过前不得自动确认。','涨价15%能直接下单吗？','超过10%阈值，须人工复核。'],
 ['机密资料撤权','撤销采购资料权限后必须停止在旧会话继续使用已读价格；新查询与导出同样重新校验。','权限取消后旧聊天还能接着查吗？','不能，旧会话需锁定或重新建立授权上下文。']
 ]};
const files=[];
for(const [group,docs]of Object.entries(groups))for(let i=0;i<docs.length;i++){
 const [title,rule,question,answer]=docs[i],id=`KB-${group}-${String(i+1).padStart(3,'0')}`,relative=`md/${id}.md`;
 const content=`# ${id} ${title}\n\n文档版本：2026-11-06\n适用租户：SC001\n资料性质：合成企业制度，用于测试。\n\n## 生效规则\n\n${rule}\n\n## 常见问题\n\n问：${question}\n\n答：${answer}\n\n## 证据与权限\n\n本文件只说明制度。实时工资、库存、订单状态以授权业务查询为准。${group==='PROC'?'本文件属于采购机密，采用上传者私有权限。':'本文件可供SC001租户内授权员工阅读。'}\n`;
 await fs.mkdir(path.dirname(path.join(out,relative)),{recursive:true});await fs.writeFile(path.join(out,relative),content);
 files.push({path:relative,title:`${id} ${title}`,scope:group==='PROC'?'self':'tenant',owner:group==='PROC'?'scale_c':'scale_admin',tenant:'SC001',sha256:createHash('sha256').update(content).digest('hex'),oracle:{question,answer}});
}
const workbooks=[{path:'hardware-catalog-50000.xlsx',title:'SC001 五金历史报价目录 50000行',scope:'tenant',owner:'scale_admin',tenant:'SC001',sheets:{Catalog:{rows:50000,columns:9},AfterSales:{rows:8,columns:6}},oracle:{latestMonth:'2026-11',sampleSku:'HW-002499',latestPrice:888.25,previousMonthPrice:888.15,latestExcelRow:50000,previousExcelRow:47500}},
 {path:'hardware-procurement-private.xlsx',title:'SC001 采购私有报价与供应商条款',scope:'self',owner:'scale_c',tenant:'SC001',sheets:{Procurement:{rows:24,columns:8},SupplierTerms:{rows:4,columns:4}},oracle:{sku:'HW-000001',basePrice:38,quantity:120,vatRate:.13,taxedUnit:42.94,lineTotal:5152.8,formulaCells:['Procurement!F2','Procurement!G2']}}];
for(const book of workbooks)book.sha256=createHash('sha256').update(await fs.readFile(path.join(out,book.path))).digest('hex');
await fs.writeFile(path.join(out,'manifest.json'),JSON.stringify({version:'2026-11-06',synthetic:true,tenant:'SC001',rowGrain:'Catalog: one SKU per quote_month, 2500 SKUs x 20 months',source:'Deterministic fixture generation; current anchor prices match scale database',workbooks,documents:files},null,2));
console.log(JSON.stringify({workbooks:workbooks.length,markdownDocuments:files.length,catalogRows:catalogRows.length,complete:true}));
