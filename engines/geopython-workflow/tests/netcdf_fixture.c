/* Independent native fixture writer and deliberate structural counterexamples.
 * Never packaged into the product. Values are verified against Python oracles. */
#define H5_USE_110_API
#include <hdf5.h>
#include <hdf5_hl.h>
#include <netcdf.h>
#include <math.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#define OK(x) do { if((x)<0){fprintf(stderr,"fixture failed: %s\n",#x);return 2;} }while(0)

static int create(const char *path,const char *mode) {
    int nc,axes[3],temp,time,kind=NC_INT,yvar=-1,xvar=-1,crs=-1;long long values[24];
    if(!strcmp(mode,"int64"))kind=NC_INT64;
    if(!strcmp(mode,"uint64"))kind=NC_UINT64;
    if(!strcmp(mode,"float32"))kind=NC_FLOAT;
    if(!strcmp(mode,"float64"))kind=NC_DOUBLE;
    int flags=NC_NETCDF4|(!strcmp(mode,"classic_model")?NC_CLASSIC_MODEL:0);
    OK(nc_create(path,flags|NC_CLOBBER,&nc));
    OK(nc_def_dim(nc,"time",2,&axes[0]));OK(nc_def_dim(nc,"y",3,&axes[1]));OK(nc_def_dim(nc,"x",4,&axes[2]));
    OK(nc_def_var(nc,"temperature",kind,3,axes,&temp));
    if(strcmp(mode,"contiguous")) {
        size_t chunks[3]={1,3,4};OK(nc_def_var_chunking(nc,temp,NC_CHUNKED,chunks));
        OK(nc_def_var_deflate(nc,temp,1,1,1));OK(nc_def_var_fletcher32(nc,temp,1));
    }
    if(kind==NC_UINT64) {unsigned long long fill=18446744073709551615ULL;OK(nc_put_att_ulonglong(nc,temp,"_FillValue",kind,1,&fill));}
    else {long long fill=-9999;OK(nc_put_att_longlong(nc,temp,"_FillValue",kind,1,&fill));}
    double scale=.5,offset=273.15;
    OK(nc_put_att_double(nc,temp,"scale_factor",NC_DOUBLE,1,&scale));
    OK(nc_put_att_double(nc,temp,"add_offset",NC_DOUBLE,1,&offset));
    OK(nc_put_att_text(nc,temp,"units",1,"K"));
    OK(nc_def_var(nc,"time",NC_INT,1,axes,&time));
    OK(nc_put_att_text(nc,time,"calendar",7,"360_day"));
    OK(nc_put_att_text(nc,time,"units",21,"days since 2000-01-01"));
    if(!strcmp(mode,"projected")) {
        const char *wkt="PROJCS[\"WGS 84 / UTM zone 50N\",GEOGCS[\"WGS 84\",DATUM[\"WGS_1984\",SPHEROID[\"WGS 84\",6378137,298.257223563]],PRIMEM[\"Greenwich\",0],UNIT[\"degree\",0.0174532925199433]],PROJECTION[\"Transverse_Mercator\"],PARAMETER[\"latitude_of_origin\",0],PARAMETER[\"central_meridian\",117],PARAMETER[\"scale_factor\",0.9996],PARAMETER[\"false_easting\",500000],PARAMETER[\"false_northing\",0],UNIT[\"metre\",1],AUTHORITY[\"EPSG\",\"32650\"]]";
        OK(nc_def_var(nc,"y",NC_DOUBLE,1,&axes[1],&yvar));OK(nc_def_var(nc,"x",NC_DOUBLE,1,&axes[2],&xvar));
        OK(nc_def_var(nc,"crs",NC_INT,0,NULL,&crs));
        OK(nc_put_att_text(nc,temp,"grid_mapping",3,"crs"));
        OK(nc_put_att_text(nc,crs,"spatial_ref",strlen(wkt),wkt));
        OK(nc_put_att_text(nc,crs,"grid_mapping_name",19,"transverse_mercator"));
        OK(nc_put_att_text(nc,yvar,"axis",1,"Y"));OK(nc_put_att_text(nc,xvar,"axis",1,"X"));
        OK(nc_put_att_text(nc,yvar,"standard_name",23,"projection_y_coordinate"));OK(nc_put_att_text(nc,xvar,"standard_name",23,"projection_x_coordinate"));
        OK(nc_put_att_text(nc,yvar,"units",1,"m"));OK(nc_put_att_text(nc,xvar,"units",1,"m"));
    }
    OK(nc_enddef(nc));int times[2]={0,30};OK(nc_put_var_int(nc,time,times));
    if(crs>=0) {
        double y[3]={3000000.,3000010.,3000020.},x[4]={500000.,500010.,500020.,500030.};int zero=0;
        OK(nc_put_var_double(nc,yvar,y));OK(nc_put_var_double(nc,xvar,x));OK(nc_put_var_int(nc,crs,&zero));
    }
    for(int i=0;i<24;i++)values[i]=(kind==NC_INT64?9007199254740993LL:0)+i;
    values[6]=-9999;
    if(kind==NC_UINT64) {
        unsigned long long u[24];for(int i=0;i<24;i++)u[i]=18446744073709551580ULL+i;
        u[6]=18446744073709551615ULL;OK(nc_put_var_ulonglong(nc,temp,u));
    } else if(kind==NC_FLOAT || kind==NC_DOUBLE) {
        double data[24];for(int i=0;i<24;i++)data[i]=i+.25;data[3]=NAN;data[6]=-9999.;
        OK(nc_put_var_double(nc,temp,data));
    } else OK(nc_put_var_longlong(nc,temp,values));
    OK(nc_close(nc));return 0;
}

int main(int argc,char **argv) {
    if(argc<3)return 2;
    OK(H5PLset_loading_state(0));
    if(!strcmp(argv[1],"create"))return create(argv[2],argc>3?argv[3]:"int32");
    hid_t f=H5Fopen(argv[2],H5F_ACC_RDWR,H5P_DEFAULT);OK(f);
    const char *mode=argv[1];
    if(!strcmp(mode,"external_link")) {
        OK(H5Lcreate_external(argv[3],"/temperature",f,"external",H5P_DEFAULT,H5P_DEFAULT));
    } else if(!strcmp(mode,"soft_link")) {
        OK(H5Lcreate_soft("/temperature",f,"alias",H5P_DEFAULT,H5P_DEFAULT));
    } else if(!strcmp(mode,"alias")) {
        OK(H5Lcreate_hard(f,"temperature",f,"alias",H5P_DEFAULT,H5P_DEFAULT));
    } else if(!strcmp(mode,"group")) {
        hid_t g=H5Gcreate2(f,"nested",H5P_DEFAULT,H5P_DEFAULT,H5P_DEFAULT);OK(g);H5Gclose(g);
    } else if(!strcmp(mode,"reference_attr")) {
        hobj_ref_t ref;OK(H5Rcreate(&ref,f,"/temperature",H5R_OBJECT,-1));
        hid_t s=H5Screate(H5S_SCALAR),a=H5Acreate2(f,"unexplained_ref",H5T_STD_REF_OBJ,s,H5P_DEFAULT,H5P_DEFAULT);OK(a);
        OK(H5Awrite(a,H5T_STD_REF_OBJ,&ref));H5Aclose(a);H5Sclose(s);
    } else if(!strcmp(mode,"vlen_text_attribute")) {
        hid_t s=H5Screate(H5S_SCALAR),t=H5Tcopy(H5T_C_S1);OK(H5Tset_size(t,H5T_VARIABLE));
        hid_t a=H5Acreate2(f,"unbounded_text",t,s,H5P_DEFAULT,H5P_DEFAULT);OK(a);const char *text="value";
        OK(H5Awrite(a,t,&text));H5Aclose(a);H5Tclose(t);H5Sclose(s);
    } else if(!strcmp(mode,"oversized_attribute")) {
        hsize_t n=4097;hid_t s=H5Screate_simple(1,&n,NULL),a=H5Acreate2(f,"too_many_values",H5T_STD_I32LE,s,H5P_DEFAULT,H5P_DEFAULT);OK(a);
        H5Aclose(a);H5Sclose(s);
    } else if(!strcmp(mode,"compound") || !strcmp(mode,"enum") || !strcmp(mode,"vlen")) {
        hid_t s=H5Screate(H5S_SCALAR),t;
        if(!strcmp(mode,"compound")) {t=H5Tcreate(H5T_COMPOUND,sizeof(int));OK(H5Tinsert(t,"member",0,H5T_STD_I32LE));}
        else if(!strcmp(mode,"enum")) {t=H5Tenum_create(H5T_STD_I32LE);int value=1;OK(H5Tenum_insert(t,"one",&value));}
        else t=H5Tvlen_create(H5T_STD_I32LE);
        hid_t d=H5Dcreate2(f,"unsupported",t,s,H5P_DEFAULT,H5P_DEFAULT,H5P_DEFAULT);OK(d);H5Dclose(d);H5Tclose(t);H5Sclose(s);
    } else if(!strcmp(mode,"multiple_scales")) {
        hid_t d=H5Dopen2(f,"temperature",H5P_DEFAULT),scale=H5Dopen2(f,"y",H5P_DEFAULT);OK(d);OK(scale);
        OK(H5DSattach_scale(d,scale,0));H5Dclose(scale);H5Dclose(d);
    } else if(!strcmp(mode,"wrong_backref") || !strcmp(mode,"duplicate_backrefs")) {
        typedef struct {hobj_ref_t dataset;unsigned dimension;} Ref;
        hid_t d=H5Dopen2(f,"x",H5P_DEFAULT),a=H5Aopen(d,"REFERENCE_LIST",H5P_DEFAULT),t=H5Tcreate(H5T_COMPOUND,sizeof(Ref));OK(a);
        OK(H5Tinsert(t,"dataset",HOFFSET(Ref,dataset),H5T_STD_REF_OBJ));OK(H5Tinsert(t,"dimension",HOFFSET(Ref,dimension),H5T_NATIVE_UINT));
        Ref refs[2];OK(H5Aread(a,t,refs));H5Aclose(a);
        if(!strcmp(mode,"wrong_backref")) {refs[0].dimension=1;a=H5Aopen(d,"REFERENCE_LIST",H5P_DEFAULT);}
        else {refs[1]=refs[0];OK(H5Adelete(d,"REFERENCE_LIST"));hsize_t n=2;hid_t s=H5Screate_simple(1,&n,NULL);a=H5Acreate2(d,"REFERENCE_LIST",t,s,H5P_DEFAULT,H5P_DEFAULT);H5Sclose(s);}
        OK(a);OK(H5Awrite(a,t,refs));H5Aclose(a);H5Tclose(t);H5Dclose(d);
    } else if(!strcmp(mode,"missing_dimensions")) {
        hid_t d=H5Dopen2(f,"temperature",H5P_DEFAULT);OK(d);OK(H5Adelete(d,"DIMENSION_LIST"));H5Dclose(d);
    } else if(!strcmp(mode,"missing_backrefs")) {
        hid_t d=H5Dopen2(f,"x",H5P_DEFAULT);OK(d);OK(H5Adelete(d,"REFERENCE_LIST"));H5Dclose(d);
    } else if(!strcmp(mode,"wrong_dimid")) {
        hid_t d=H5Dopen2(f,"x",H5P_DEFAULT),a=H5Aopen(d,"_Netcdf4Dimid",H5P_DEFAULT);int id=77;OK(a);
        OK(H5Awrite(a,H5T_NATIVE_INT,&id));H5Aclose(a);H5Dclose(d);
    } else if(!strcmp(mode,"dangling_dimension")) {
        hid_t d=H5Dopen2(f,"temperature",H5P_DEFAULT),a=H5Aopen(d,"DIMENSION_LIST",H5P_DEFAULT),t=H5Aget_type(a);
        hobj_ref_t ref=0;hvl_t refs[3]={{1,&ref},{1,&ref},{1,&ref}};OK(H5Awrite(a,t,refs));H5Tclose(t);H5Aclose(a);H5Dclose(d);
    } else {
        hsize_t shape[2]={3,4};hid_t s=H5Screate_simple(2,shape,NULL),p=H5Pcreate(H5P_DATASET_CREATE);
        if(!strcmp(mode,"external_raw")) {OK(H5Pset_external(p,argv[3],0,48));}
        else if(!strcmp(mode,"virtual")) {OK(H5Pset_virtual(p,s,argv[3],"/temperature",s));}
        else if(!strcmp(mode,"unknown_filter")) {OK(H5Pset_chunk(p,2,shape));OK(H5Pset_filter(p,65000,H5Z_FLAG_OPTIONAL,0,NULL));}
        else if(!strcmp(mode,"oversized_chunk")) {hsize_t huge[2]={8192,8192};H5Sclose(s);s=H5Screate_simple(2,huge,NULL);OK(s);OK(H5Pset_chunk(p,2,huge));}
        else return 2;
        hid_t d=H5Dcreate2(f,"auxiliary",H5T_STD_I32LE,s,H5P_DEFAULT,p,H5P_DEFAULT);OK(d);
        H5Dclose(d);H5Pclose(p);H5Sclose(s);
    }
    OK(H5Fclose(f));return 0;
}
